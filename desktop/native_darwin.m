//go:build darwin

#import <Cocoa/Cocoa.h>
#import <WebKit/WebKit.h>
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

static int decide(WKWebView *webView, NSURL *url, int flags) {
	return hikyoNavigate((char *)pageHost(webView).UTF8String, (char *)url.absoluteString.UTF8String, flags);
}

static void decideAction(id self, SEL _cmd, WKWebView *webView, WKNavigationAction *action, void (^handler)(WKNavigationActionPolicy)) {
	int flags = 0;
	if (action.targetFrame.isMainFrame) flags |= flagMainFrame;
	if (action.targetFrame == nil) flags |= flagNewWindow;
	if (action.shouldPerformDownload) flags |= flagDownload;
	if (action.navigationType == WKNavigationTypeLinkActivated || action.navigationType == WKNavigationTypeFormSubmitted) flags |= flagGesture;
	int verdict = decide(webView, action.request.URL, flags);
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
	int verdict = decide(webView, navResponse.response.URL, flagDownload | flagMainFrame);
	handler(verdict == 2 ? WKNavigationResponsePolicyDownload : WKNavigationResponsePolicyCancel);
}

static WKWebView *createWindow(id self, SEL _cmd, WKWebView *webView, WKWebViewConfiguration *configuration, WKNavigationAction *action, WKWindowFeatures *features) {
	// A popup never gets a window of its own: https leaves for the browser.
	decide(webView, action.request.URL, flagNewWindow | flagMainFrame);
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
	});
}

char *hikyoSavePanel(const char *name, const char *source) {
	__block char *out = NULL;
	NSString *n = [NSString stringWithUTF8String:name], *from = [NSString stringWithUTF8String:source];
	onMainRL(^{
		NSSavePanel *panel = [NSSavePanel savePanel];
		panel.nameFieldStringValue = n;
		panel.message = [NSString stringWithFormat:@"Save a file from %@", from];
		panel.canCreateDirectories = YES;
		[NSApp activateIgnoringOtherApps:YES];
		if ([panel runModal] == NSModalResponseOK && panel.URL.path != nil) {
			out = strdup(panel.URL.path.UTF8String);
		}
	});
	return out;
}

static NSMutableDictionary<NSNumber *, NSAlert *> *codeAlerts;

void hikyoShowCode(long id, const char *url, const char *code) {
	NSString *u = [NSString stringWithUTF8String:url], *c = [NSString stringWithUTF8String:code];
	asyncMainRL(^{
		if (codeAlerts == nil) codeAlerts = [NSMutableDictionary new];
		NSAlert *alert = [NSAlert new];
		alert.messageText = @"Finish signing in in your browser";
		alert.informativeText = [NSString stringWithFormat:@"Open %@ and enter this code:\n\n%@\n\nOnly enter it if you started this sign-in.", u, c];
		[alert addButtonWithTitle:@"Cancel"];
		codeAlerts[@(id)] = alert;
		[NSApp activateIgnoringOtherApps:YES];
		[alert runModal];
		if (codeAlerts[@(id)] != nil) {
			// Closed by the user, not by dismissCode.
			[codeAlerts removeObjectForKey:@(id)];
			hikyoCodeCancelled(id);
		}
	});
}

void hikyoDismissCode(long id) {
	dispatch_async(dispatch_get_main_queue(), ^{
		if (codeAlerts[@(id)] != nil) {
			[codeAlerts removeObjectForKey:@(id)];
			[NSApp abortModal];
		}
	});
}

// ---- Pasteboard ----

long hikyoPasteboardCount(void) {
	__block long count = 0;
	onMain(^{ count = NSPasteboard.generalPasteboard.changeCount; });
	return count;
}

int hikyoAppActive(void) {
	__block int active = 0;
	onMain(^{ active = NSApp.isActive; });
	return active;
}

void hikyoClearPasteboard(void) {
	onMain(^{ [NSPasteboard.generalPasteboard clearContents]; });
}

// ---- Storage ----

// Removes the website data of one connection host. Returns the number of
// records removed, or -1 when the data store did not answer. Must not run on
// the main thread: WebKit answers there.
int hikyoWipeStorage(const char *host) {
	NSString *needle = [NSString stringWithUTF8String:host];
	dispatch_semaphore_t done = dispatch_semaphore_create(0);
	__block int removed = -1;
	dispatch_async(dispatch_get_main_queue(), ^{
		WKWebsiteDataStore *store = WKWebsiteDataStore.defaultDataStore;
		NSSet *types = WKWebsiteDataStore.allWebsiteDataTypes;
		[store fetchDataRecordsOfTypes:types completionHandler:^(NSArray<WKWebsiteDataRecord *> *records) {
			NSMutableArray *match = [NSMutableArray new];
			for (WKWebsiteDataRecord *record in records) {
				if ([record.displayName containsString:needle]) [match addObject:record];
			}
			[store removeDataOfTypes:types forDataRecords:match completionHandler:^{
				removed = (int)match.count;
				dispatch_semaphore_signal(done);
			}];
		}];
	});
	if (dispatch_semaphore_wait(done, dispatch_time(DISPATCH_TIME_NOW, 15 * NSEC_PER_SEC)) != 0) return -1;
	return removed;
}
