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

void spikeReplacePolicy(void) {
	Class c = objc_getClass("WailsViewController");
	Method m = class_getInstanceMethod(c, @selector(webView:decidePolicyForNavigationAction:decisionHandler:));
	class_replaceMethod(c, @selector(webView:decidePolicyForNavigationAction:decisionHandler:), (IMP)decideAction, method_getTypeEncoding(m));
	note(@"navigation policy replaced on %s", class_getName(c));
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
