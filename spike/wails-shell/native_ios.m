//go:build ios

#import <UIKit/UIKit.h>
#import <WebKit/WebKit.h>
#import <AuthenticationServices/AuthenticationServices.h>
#import <objc/runtime.h>
#include "_cgo_export.h"

static void note(NSString *format, ...) NS_FORMAT_FUNCTION(1, 2);
static void note(NSString *format, ...) {
	va_list args;
	va_start(args, format);
	NSString *text = [[NSString alloc] initWithFormat:format arguments:args];
	va_end(args);
	spikeLog((char *)text.UTF8String);
}

// Wails' iOS delegate (WailsViewController) already has this method and allows everything, so it is replaced, not added to.
static void decideAction(id self, SEL _cmd, WKWebView *webView, WKNavigationAction *action, void (^handler)(WKNavigationActionPolicy)) {
	NSURL *url = action.request.URL;
	NSString *scheme = url.scheme.lowercaseString;
	BOOL newWindow = action.targetFrame == nil;
	BOOL mainFrame = action.targetFrame.isMainFrame;
	WKNavigationActionPolicy policy = WKNavigationActionPolicyCancel;
	NSString *why = @"other";
	if ([scheme isEqualToString:@"wails"]) {
		if (webView.URL.host == nil || [webView.URL.host isEqualToString:url.host]) policy = WKNavigationActionPolicyAllow;
		why = @"wails";
	} else if ([scheme isEqualToString:@"about"] && [url.absoluteString isEqualToString:@"about:blank"]) {
		policy = WKNavigationActionPolicyAllow;
		why = @"about:blank";
	} else if ([scheme isEqualToString:@"http"] || [scheme isEqualToString:@"https"]) {
		if (mainFrame || newWindow) spikeOpenURL((char *)url.absoluteString.UTF8String);
		why = @"web";
	}
	note(@"navigation %@ (%@, main=%d new=%d download=%d) -> %@", url.absoluteString, why, mainFrame, newWindow, action.shouldPerformDownload,
		policy == WKNavigationActionPolicyAllow ? @"allow" : @"cancel");
	handler(policy);
}

// Wails serves /__capture__/<name> from the app's temp directory before the request reaches Go, so none of the shell's
// checks would see it. A connection's page has no use for it.
static IMP originalStart;
static void startTask(id self, SEL _cmd, WKWebView *webView, id<WKURLSchemeTask> task) {
	if ([task.request.URL.path hasPrefix:@"/__capture__/"]) {
		note(@"refused %@", task.request.URL.absoluteString);
		[task didFailWithError:[NSError errorWithDomain:NSURLErrorDomain code:NSURLErrorResourceUnavailable userInfo:nil]];
		return;
	}
	((void (*)(id, SEL, WKWebView *, id<WKURLSchemeTask>))originalStart)(self, _cmd, webView, task);
}

// Returns 1 once the handlers are in place. The isonopolicy build leaves Wails' allow-everything navigation method alone,
// to show what stops a navigation without it.
int spikeReplacePolicy(void) {
	Class handler = objc_getClass("WailsSchemeHandler");
	if (handler == nil) return 0;
	Method start = class_getInstanceMethod(handler, @selector(webView:startURLSchemeTask:));
	if (start == nil) return 0;
	originalStart = method_setImplementation(start, (IMP)startTask);
#ifdef ISO_NOPOLICY
	return 1;
#endif
	Class c = objc_getClass("WailsViewController");
	if (c == nil) return 0;
	Method m = class_getInstanceMethod(c, @selector(webView:decidePolicyForNavigationAction:decisionHandler:));
	if (m == nil) return 0;
	class_replaceMethod(c, @selector(webView:decidePolicyForNavigationAction:decisionHandler:), (IMP)decideAction, method_getTypeEncoding(m));
	note(@"navigation policy replaced on %s", class_getName(c));
	return 1;
}

// One segment per connection, in the order the windows were made. Choosing one makes that connection's view controller
// the root; the others stay alive off screen, so their pages keep their state.
@interface SpikeSwitcher : NSObject
- (void)pick:(UISegmentedControl *)control;
@end
@implementation SpikeSwitcher
- (void)pick:(UISegmentedControl *)control {
	id delegate = UIApplication.sharedApplication.delegate;
	NSArray *controllers = [delegate valueForKey:@"viewControllers"];
	UIWindow *window = [delegate valueForKey:@"window"];
	NSInteger i = control.selectedSegmentIndex;
	if (i < 0 || (NSUInteger)i >= controllers.count) return;
	window.rootViewController = controllers[i];
	[window bringSubviewToFront:control];
	note(@"switched to connection %ld", (long)i);
}
@end

static SpikeSwitcher *switcher;

void spikeInstallSwitcher(const char *const *labels, int n) {
	NSMutableArray<NSString *> *titles = [NSMutableArray array];
	for (int i = 0; i < n; i++) [titles addObject:[NSString stringWithUTF8String:labels[i]]];
	if (n < 2) return;
	__block int tries = 0;
	__block void (^attempt)(void) = ^{
		id delegate = UIApplication.sharedApplication.delegate;
		NSArray *controllers = [delegate valueForKey:@"viewControllers"];
		UIWindow *window = [delegate valueForKey:@"window"];
		if (window == nil || controllers.count < titles.count) {
			// The views are made after launch, so wait for them.
			if (tries++ < 100) dispatch_after(dispatch_time(DISPATCH_TIME_NOW, 100 * NSEC_PER_MSEC), dispatch_get_main_queue(), attempt);
			else note(@"switcher: views never appeared");
			return;
		}
		switcher = [SpikeSwitcher new];
		UISegmentedControl *control = [[UISegmentedControl alloc] initWithItems:titles];
		control.selectedSegmentIndex = 0;
		control.accessibilityLabel = @"Connections";
		CGRect b = window.bounds;
		control.frame = CGRectMake(16, b.size.height - window.safeAreaInsets.bottom - 44, b.size.width - 32, 32);
		control.autoresizingMask = UIViewAutoresizingFlexibleWidth | UIViewAutoresizingFlexibleTopMargin;
		[control addTarget:switcher action:@selector(pick:) forControlEvents:UIControlEventValueChanged];
		[window addSubview:control];
		note(@"switcher installed for %lu connections", (unsigned long)titles.count);
		attempt = nil;
	};
	dispatch_async(dispatch_get_main_queue(), attempt);
}

@interface SpikeAnchor : NSObject <ASWebAuthenticationPresentationContextProviding>
@end
@implementation SpikeAnchor
- (ASPresentationAnchor)presentationAnchorForWebAuthenticationSession:(ASWebAuthenticationSession *)session {
	for (UIScene *scene in UIApplication.sharedApplication.connectedScenes) {
		if ([scene isKindOfClass:UIWindowScene.class]) {
			for (UIWindow *w in ((UIWindowScene *)scene).windows) if (w.isKeyWindow) return w;
		}
	}
	return UIApplication.sharedApplication.windows.firstObject;
}
@end

static ASWebAuthenticationSession *session;
static SpikeAnchor *anchor;

void spikeASESStart(const char *raw) {
	NSURL *url = [NSURL URLWithString:[NSString stringWithUTF8String:raw]];
	dispatch_async(dispatch_get_main_queue(), ^{
		anchor = [SpikeAnchor new];
		// The callback scheme is never used: the server only accepts an http loopback redirect, so completion has to come from Go.
		session = [[ASWebAuthenticationSession alloc] initWithURL:url callbackURLScheme:@"hikyo-spike-unused" completionHandler:^(NSURL *cb, NSError *err) {
			note(@"ases completion url=%@ error=%@", cb, err);
		}];
		session.prefersEphemeralWebBrowserSession = NO;
		session.presentationContextProvider = anchor;
		BOOL started = [session start];
		note(@"ases start=%d", started);
	});
}

void spikeASESCancel(void) {
	dispatch_async(dispatch_get_main_queue(), ^{
		note(@"ases cancel (session=%@)", session);
		[session cancel];
	});
}
