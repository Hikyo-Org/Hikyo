//go:build darwin

#import <Cocoa/Cocoa.h>
#import <WebKit/WebKit.h>
#import <UniformTypeIdentifiers/UniformTypeIdentifiers.h>
#import <objc/runtime.h>
#include "_cgo_export.h"

// Everything here runs the AppKit work on the main thread. The Go side calls
// from goroutines, so a call that is already on the main thread (a WebKit
// delegate) runs inline.
static void onMain(void (^block)(void)) {
	if ([NSThread isMainThread]) {
		block();
	} else {
		dispatch_sync(dispatch_get_main_queue(), block);
	}
}

// onMainRL runs block on the main thread from a run loop source rather than
// the GCD main queue. A modal dialog opened inside a GCD main-queue block
// stops libdispatch from draining the main queue until it closes, which would
// stall every other onMain caller (and so the clipboard timer).
static void onMainRL(void (^block)(void)) {
	if ([NSThread isMainThread]) {
		block();
		return;
	}
	dispatch_semaphore_t done = dispatch_semaphore_create(0);
	CFRunLoopPerformBlock(CFRunLoopGetMain(), kCFRunLoopCommonModes, ^{
		block();
		dispatch_semaphore_signal(done);
	});
	CFRunLoopWakeUp(CFRunLoopGetMain());
	dispatch_semaphore_wait(done, DISPATCH_TIME_FOREVER);
}

static void asyncMainRL(void (^block)(void)) {
	CFRunLoopPerformBlock(CFRunLoopGetMain(), kCFRunLoopCommonModes, block);
	CFRunLoopWakeUp(CFRunLoopGetMain());
}

static void reapDismissed(void);

static void note(NSString *format, ...) NS_FORMAT_FUNCTION(1, 2);
static void note(NSString *format, ...) {
	va_list args;
	va_start(args, format);
	NSString *text = [[NSString alloc] initWithFormat:format arguments:args];
	va_end(args);
	hikyoLog((char *)text.UTF8String);
}

// ---- WebKit delegate methods Wails v3.1.0 lacks ----

enum {
	flagMainFrame = 1,
	flagNewWindow = 2,
	flagDownload = 4,
	flagGesture = 8,
};

static NSString *pageHost(WKWebView *webView) {
	return webView.URL.host.lowercaseString ?: @"";
}

// Saves a blob download where the user chooses. Go shows the save panel.
@interface HikyoDownloads : NSObject <WKDownloadDelegate>
@end

@implementation HikyoDownloads
- (void)download:(WKDownload *)download decideDestinationUsingResponse:(NSURLResponse *)response suggestedFilename:(NSString *)name completionHandler:(void (^)(NSURL *))handler {
	NSString *host = pageHost(download.webView);
	char *chosen = hikyoDownloadDestination((uintptr_t)(__bridge void *)download, (char *)host.UTF8String, (char *)name.UTF8String);
	if (chosen == NULL) {
		handler(nil); // cancels the download
		return;
	}
	NSURL *destination = [NSURL fileURLWithPath:[NSString stringWithUTF8String:chosen]];
	free(chosen);
	// Go chose a fresh temporary file next to the user's choice; it is marked
	// and renamed into place when the download finishes.
	handler(destination);
}
- (void)downloadDidFinish:(WKDownload *)download {
	hikyoDownloadFinished((uintptr_t)(__bridge void *)download);
}
- (void)download:(WKDownload *)download didFailWithError:(NSError *)error resumeData:(NSData *)resumeData {
	hikyoDownloadFailed((uintptr_t)(__bridge void *)download, (char *)error.localizedDescription.UTF8String);
}
@end

static HikyoDownloads *downloads;

static void becameDownload(id self, SEL _cmd, WKWebView *webView, id trigger, WKDownload *download) {
	download.delegate = downloads;
}

static int decide(WKWebView *webView, NSURL *url, int flags, NSString *method, NSString *filename) {
	return hikyoNavigate((char *)pageHost(webView).UTF8String, (char *)url.absoluteString.UTF8String, flags,
		(char *)(method ?: @"").UTF8String, (char *)(filename ?: @"").UTF8String);
}

// The method of the last main-frame navigation a window started. WebKit does
// not tell a response which request it answers, and the shell must not replay
// a form POST as a GET.
static const void *kLastMethod = &kLastMethod;

// The link's download attribute. WebKit keeps it behind a private property;
// without it the file is named from the response or the URL, so a missing
// property only costs the suggested name.
static NSString *downloadAttribute(WKNavigationAction *action) {
	SEL sel = NSSelectorFromString(@"_downloadAttribute");
	if (![action respondsToSelector:sel]) return nil;
	id value = [action valueForKey:@"_downloadAttribute"];
	return [value isKindOfClass:[NSString class]] ? value : nil;
}

static void decideAction(id self, SEL _cmd, WKWebView *webView, WKNavigationAction *action, void (^handler)(WKNavigationActionPolicy)) {
	int flags = 0;
	if (action.targetFrame.isMainFrame) flags |= flagMainFrame;
	if (action.targetFrame == nil) flags |= flagNewWindow;
	if (action.shouldPerformDownload) flags |= flagDownload;
	if (action.navigationType == WKNavigationTypeLinkActivated || action.navigationType == WKNavigationTypeFormSubmitted) flags |= flagGesture;
	int verdict = decide(webView, action.request.URL, flags, action.request.HTTPMethod, action.shouldPerformDownload ? downloadAttribute(action) : nil);
	if (verdict == 1 && action.targetFrame.isMainFrame) {
		// Only a navigation that will actually load can produce the next response.
		objc_setAssociatedObject(webView, kLastMethod, action.request.HTTPMethod, OBJC_ASSOCIATION_COPY_NONATOMIC);
	}
	if (verdict == 1) {
		handler(WKNavigationActionPolicyAllow);
	} else if (verdict == 2) {
		handler(WKNavigationActionPolicyDownload);
	} else {
		handler(WKNavigationActionPolicyCancel);
	}
}

// An attachment, or a type WebKit cannot show, is a download. A custom-scheme
// download cannot be handed to WKDownload (NSURLErrorDomain -1002), so Go
// fetches it through the connection's proxy and the response is cancelled.
// Go refuses it when the request that produced the response was not a GET.
static void decideResponse(id self, SEL _cmd, WKWebView *webView, WKNavigationResponse *navResponse, void (^handler)(WKNavigationResponsePolicy)) {
	NSString *disposition = nil;
	if ([navResponse.response isKindOfClass:[NSHTTPURLResponse class]]) {
		disposition = [((NSHTTPURLResponse *)navResponse.response) valueForHTTPHeaderField:@"Content-Disposition"];
	}
	NSString *kind = [disposition.lowercaseString stringByTrimmingCharactersInSet:NSCharacterSet.whitespaceCharacterSet];
	BOOL download = !navResponse.canShowMIMEType || [kind hasPrefix:@"attachment"];
	if (!download) {
		handler(WKNavigationResponsePolicyAllow);
		return;
	}
	if (!navResponse.isForMainFrame) {
		// Only a main-frame response is ever saved; a subframe's request method is unknown.
		handler(WKNavigationResponsePolicyCancel);
		return;
	}
	NSString *method = objc_getAssociatedObject(webView, kLastMethod);
	int verdict = decide(webView, navResponse.response.URL, flagDownload | flagMainFrame, method, nil);
	handler(verdict == 2 ? WKNavigationResponsePolicyDownload : WKNavigationResponsePolicyCancel);
}

static WKWebView *createWindow(id self, SEL _cmd, WKWebView *webView, WKWebViewConfiguration *configuration, WKNavigationAction *action, WKWindowFeatures *features) {
	// A popup never gets a window of its own: https leaves for the browser.
	decide(webView, action.request.URL, flagNewWindow | flagMainFrame, action.request.HTTPMethod, nil);
	return nil;
}

// Returns how many methods were added; the caller must see all of them.
int hikyoInstallDelegates(void) {
	Class delegate = objc_getClass("WebviewWindowDelegate");
	if (delegate == nil) return -1;
	downloads = [HikyoDownloads new];
	int added = 0;
	added += class_addMethod(delegate, @selector(webView:decidePolicyForNavigationAction:decisionHandler:), (IMP)decideAction, "v@:@@@?");
	added += class_addMethod(delegate, @selector(webView:decidePolicyForNavigationResponse:decisionHandler:), (IMP)decideResponse, "v@:@@@?");
	added += class_addMethod(delegate, @selector(webView:navigationAction:didBecomeDownload:), (IMP)becameDownload, "v@:@@@");
	added += class_addMethod(delegate, @selector(webView:navigationResponse:didBecomeDownload:), (IMP)becameDownload, "v@:@@@");
	added += class_addMethod(delegate, @selector(webView:createWebViewWithConfiguration:forNavigationAction:windowFeatures:), (IMP)createWindow, "@@:@@@@");
	return added;
}

// ---- Window hardening (ADR D10) ----

static WKWebView *findWebView(NSView *view) {
	if ([view isKindOfClass:[WKWebView class]]) return (WKWebView *)view;
	for (NSView *sub in view.subviews) {
		WKWebView *found = findWebView(sub);
		if (found) return found;
	}
	return nil;
}

// Removes Wails' `external` script message handler, the page's bridge into
// the Wails runtime, and turns off the Web Inspector. Returns 1 when the
// window's web view was found and hardened.
int hikyoHardenWindow(void *nswindow) {
	__block int ok = 0;
	onMain(^{
		NSWindow *window = (__bridge NSWindow *)nswindow;
		WKWebView *webView = findWebView(window.contentView);
		if (webView == nil) return;
		[webView.configuration.userContentController removeScriptMessageHandlerForName:@"external"];
		if (@available(macOS 13.3, *)) webView.inspectable = NO;
		ok = 1;
	});
	return ok;
}

// ---- Dialogs ----

int hikyoConfirm(const char *title, const char *message, const char *label) {
	__block int ok = 0;
	NSString *t = [NSString stringWithUTF8String:title], *m = [NSString stringWithUTF8String:message], *l = [NSString stringWithUTF8String:label];
	onMainRL(^{
		NSAlert *alert = [NSAlert new];
		alert.messageText = t;
		alert.informativeText = m;
		NSButton *yes = [alert addButtonWithTitle:l];
		NSButton *no = [alert addButtonWithTitle:@"Cancel"];
		// Cancel is the default, so a stray Return does not trust an instance.
		yes.keyEquivalent = @"";
		no.keyEquivalent = @"\r";
		[NSApp activateIgnoringOtherApps:YES];
		ok = [alert runModal] == NSAlertFirstButtonReturn;
		reapDismissed();
	});
	return ok;
}

void hikyoAlert(const char *title, const char *message) {
	NSString *t = [NSString stringWithUTF8String:title], *m = [NSString stringWithUTF8String:message];
	asyncMainRL(^{
		NSAlert *alert = [NSAlert new];
		alert.alertStyle = NSAlertStyleWarning;
		alert.messageText = t;
		alert.informativeText = m;
		[NSApp activateIgnoringOtherApps:YES];
		[alert runModal];
		reapDismissed();
	});
}

// replace reports that a file was at the chosen path when the panel answered
// (so the panel asked about replacing it). It is looked up in the same
// main-thread step as the answer; a file created in that instant is the one
// remaining gap.
char *hikyoSavePanel(const char *name, const char *source, const char *mime, int *replace) {
	__block char *out = NULL;
	NSString *n = [NSString stringWithUTF8String:name], *from = [NSString stringWithUTF8String:source];
	NSString *type = [NSString stringWithUTF8String:mime];
	onMainRL(^{
		NSSavePanel *panel = [NSSavePanel savePanel];
		panel.nameFieldStringValue = n;
		// The page's type hint only suggests an extension; any name stays allowed.
		UTType *hint = type.length > 0 ? [UTType typeWithMIMEType:type] : nil;
		if (hint != nil) {
			panel.allowedContentTypes = @[hint];
			panel.allowsOtherFileTypes = YES;
		}
		panel.message = [NSString stringWithFormat:@"Save a file from %@", from];
		panel.canCreateDirectories = YES;
		[NSApp activateIgnoringOtherApps:YES];
		if ([panel runModal] == NSModalResponseOK && panel.URL.path != nil) {
			out = strdup(panel.URL.path.UTF8String);
			*replace = [[NSFileManager defaultManager] fileExistsAtPath:panel.URL.path] ? 1 : 0;
		}
		reapDismissed();
	});
	return out;
}

static NSMutableDictionary<NSNumber *, NSAlert *> *codeAlerts;
// Code dialogs dismissed while another modal was on top of them: their own
// modal session is ended as soon as they are on top again (see reapDismissed).
static NSMutableSet<NSAlert *> *dismissedCodes;

static void reapDismissed(void) {
	for (NSAlert *alert in [dismissedCodes copy]) {
		if (NSApp.modalWindow == alert.window) {
			[dismissedCodes removeObject:alert];
			[NSApp abortModal];
		}
	}
}

void hikyoShowCode(long id, const char *url, const char *code) {
	NSString *u = [NSString stringWithUTF8String:url], *c = [NSString stringWithUTF8String:code];
	asyncMainRL(^{
		if (codeAlerts == nil) codeAlerts = [NSMutableDictionary new];
		NSAlert *alert = [NSAlert new];
		alert.messageText = @"Finish signing in in your browser";
		alert.informativeText = [NSString stringWithFormat:@"Open %@ and enter this code:\n\n%@\n\nOnly enter it if you started this sign-in.", u, c];
		[alert addButtonWithTitle:@"Open in browser"];
		[alert addButtonWithTitle:@"Cancel"];
		codeAlerts[@(id)] = alert;
		[NSApp activateIgnoringOtherApps:YES];
		// The code stays on screen while the browser opens: Go opens the
		// validated verification URL and the dialog runs again.
		NSModalResponse response;
		while ((response = [alert runModal]) == NSAlertFirstButtonReturn && codeAlerts[@(id)] != nil) {
			hikyoCodeOpenBrowser(id);
		}
		if (codeAlerts[@(id)] != nil) {
			// Closed by the user, not by dismissCode.
			[codeAlerts removeObjectForKey:@(id)];
			hikyoCodeCancelled(id);
		}
		[dismissedCodes removeObject:alert];
		reapDismissed();
	});
}

void hikyoDismissCode(long id) {
	// Same queue as hikyoShowCode, so a dismiss never runs before its show.
	asyncMainRL(^{
		NSAlert *alert = codeAlerts[@(id)];
		if (alert != nil) {
			[codeAlerts removeObjectForKey:@(id)];
			// Only end the modal session if this alert is the one on top;
			// otherwise a dialog opened over it would be aborted.
			if (NSApp.modalWindow == alert.window) {
				[NSApp abortModal];
			} else {
				if (dismissedCodes == nil) dismissedCodes = [NSMutableSet new];
				[dismissedCodes addObject:alert];
			}
		}
	});
}

// ---- Pasteboard ----

long hikyoPasteboardCount(void) {
	__block long count = 0;
	onMain(^{ count = NSPasteboard.generalPasteboard.changeCount; });
	return count;
}

// The change count when the app last became frontmost, taken in the
// activation notification itself so a copy made in another app is always at
// or below it, however the 250 ms poll falls.
static long activationCount;

__attribute__((constructor)) static void trackActivation(void) {
	activationCount = NSPasteboard.generalPasteboard.changeCount;
	[NSNotificationCenter.defaultCenter addObserverForName:NSApplicationDidBecomeActiveNotification object:nil queue:nil usingBlock:^(NSNotification *note) {
		activationCount = NSPasteboard.generalPasteboard.changeCount;
	}];
}

long hikyoPasteboardCountAtActivation(void) {
	__block long count = 0;
	onMain(^{ count = activationCount; });
	return count;
}

int hikyoAppActive(void) {
	__block int active = 0;
	onMain(^{ active = NSApp.isActive; });
	return active;
}

// Clears only if nothing was copied since count was read; the check and the
// clear run in one main-thread block.
void hikyoClearPasteboardIf(long count) {
	onMain(^{
		NSPasteboard *pb = NSPasteboard.generalPasteboard;
		if (pb.changeCount == count) [pb clearContents];
	});
}

// ---- Storage ----

// ---- Storage wipe ----
//
// Website data of a custom-scheme origin (wails://<host>) is not listed by
// -fetchDataRecordsOfTypes: (verified on macOS 27: localStorage survives a
// relaunch while the data store reports zero records). So the origin is
// cleared from inside: a hidden web view of the same default data store loads
// wails://<host>/ from a handler of its own and deletes what the origin holds.
@interface HikyoWipePage : NSObject <WKURLSchemeHandler>
@end

@implementation HikyoWipePage
- (void)webView:(WKWebView *)webView startURLSchemeTask:(id<WKURLSchemeTask>)task {
	NSData *body = [@"<!doctype html><title>wipe</title>" dataUsingEncoding:NSUTF8StringEncoding];
	NSURLResponse *response = [[NSURLResponse alloc] initWithURL:task.request.URL MIMEType:@"text/html" expectedContentLength:body.length textEncodingName:@"utf-8"];
	[task didReceiveResponse:response];
	[task didReceiveData:body];
	[task didFinish];
}
- (void)webView:(WKWebView *)webView stopURLSchemeTask:(id<WKURLSchemeTask>)task {}
@end

@interface HikyoWipeRun : NSObject <WKNavigationDelegate>
@property (strong) WKWebView *webView;
@property (strong) HikyoWipePage *page;
@property (copy) void (^done)(int);
- (void)finish:(int)result;
@end

static NSString *const wipeScript =
	@"localStorage.clear(); sessionStorage.clear();"
	@"if (indexedDB.databases) { for (const d of await indexedDB.databases()) {"
	@"  await new Promise((r, j) => { const q = indexedDB.deleteDatabase(d.name); q.onsuccess = r; q.onerror = () => j(q.error); }); } }"
	@"if (window.caches) { for (const k of await caches.keys()) await caches.delete(k); }"
	@"for (const c of document.cookie.split(';')) { const n = c.split('=')[0].trim();"
	@"  if (n) document.cookie = n + '=; expires=Thu, 01 Jan 1970 00:00:00 GMT; path=/'; }"
	@"return 'wiped';";

@implementation HikyoWipeRun
- (void)webView:(WKWebView *)webView didFinishNavigation:(WKNavigation *)navigation {
	[webView callAsyncJavaScript:wipeScript arguments:@{} inFrame:nil inContentWorld:WKContentWorld.pageWorld completionHandler:^(id result, NSError *error) {
		if (error != nil) NSLog(@"hikyo: storage wipe script failed: %@", error);
		[self finish:error == nil && [result isEqual:@"wiped"] ? 1 : -1];
	}];
}
- (void)webView:(WKWebView *)webView didFailProvisionalNavigation:(WKNavigation *)navigation withError:(NSError *)error {
	NSLog(@"hikyo: storage wipe page failed: %@", error);
	[self finish:-1];
}
- (void)webView:(WKWebView *)webView didFailNavigation:(WKNavigation *)navigation withError:(NSError *)error {
	[self finish:-1];
}
// The first outcome wins; late callbacks after the caller left are ignored.
- (void)finish:(int)result {
	void (^done)(int) = self.done;
	self.done = nil;
	if (done != nil) done(result);
}
@end

// Deletes the browser storage of one connection host. Returns 1 when done and
// -1 when it could not be confirmed. Must not run on the main thread.
int hikyoWipeStorage(const char *host) {
	NSString *name = [NSString stringWithUTF8String:host];
	dispatch_semaphore_t done = dispatch_semaphore_create(0);
	__block int result = -1;
	HikyoWipeRun *keep = [HikyoWipeRun new];
	keep.done = ^(int r) {
		result = r;
		dispatch_semaphore_signal(done);
	};
	dispatch_async(dispatch_get_main_queue(), ^{
		WKWebViewConfiguration *config = [WKWebViewConfiguration new];
		keep.page = [HikyoWipePage new];
		[config setURLSchemeHandler:keep.page forURLScheme:@"wails"];
		keep.webView = [[WKWebView alloc] initWithFrame:NSMakeRect(0, 0, 10, 10) configuration:config];
		keep.webView.navigationDelegate = keep;
		[keep.webView loadRequest:[NSURLRequest requestWithURL:[NSURL URLWithString:[NSString stringWithFormat:@"wails://%@/", name]]]];
	});
	int waited = dispatch_semaphore_wait(done, dispatch_time(DISPATCH_TIME_NOW, 15 * NSEC_PER_SEC));
	dispatch_async(dispatch_get_main_queue(), ^{ keep.webView = nil; [keep finish:-1]; });
	return waited != 0 ? -1 : result;
}
