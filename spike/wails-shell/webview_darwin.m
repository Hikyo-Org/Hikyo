//go:build darwin && !ios

#import <Cocoa/Cocoa.h>
#import <WebKit/WebKit.h>
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

static BOOL isWeb(NSString *scheme) {
	return [scheme isEqualToString:@"http"] || [scheme isEqualToString:@"https"];
}

static void openOutside(NSURL *url) {
	spikeOpenURL((char *)url.absoluteString.UTF8String);
}

// Saves into the directory Go chooses and never overwrites.
@interface SpikeDownloads : NSObject <WKDownloadDelegate>
@end

@implementation SpikeDownloads
- (void)download:(WKDownload *)download decideDestinationUsingResponse:(NSURLResponse *)response suggestedFilename:(NSString *)name completionHandler:(void (^)(NSURL *))handler {
	char *chosen = spikeDownloadPath((char *)name.UTF8String);
	if (chosen == NULL) {
		handler(nil); // nil cancels the download
		return;
	}
	NSURL *destination = [NSURL fileURLWithPath:[NSString stringWithUTF8String:chosen]];
	free(chosen);
	note(@"download starts: %@ -> %@ (%@, %lld bytes expected)", response.URL, destination.path, response.MIMEType, response.expectedContentLength);
	handler(destination);
}
- (void)downloadDidFinish:(WKDownload *)download {
	note(@"download finished: %@", download.originalRequest.URL);
}
- (void)download:(WKDownload *)download didFailWithError:(NSError *)error resumeData:(NSData *)resumeData {
	note(@"download FAILED: %@ error=%@", download.originalRequest.URL, error);
}
@end

static SpikeDownloads *downloads;

static void becameDownload(id self, SEL _cmd, WKWebView *webView, id trigger, WKDownload *download) {
	download.delegate = downloads;
	note(@"became download");
}

// Same host as the page's own, so a window cannot be moved to another connection's origin.
static BOOL sameConnection(WKWebView *webView, NSURL *url) {
#ifdef ISO_NOPOLICY
	return YES; // control build: shows what stops a navigation without this check
#endif
	return webView.URL.host == nil || [webView.URL.host isEqualToString:url.host];
}

static void decideAction(id self, SEL _cmd, WKWebView *webView, WKNavigationAction *action, void (^handler)(WKNavigationActionPolicy)) {
	NSURL *url = action.request.URL;
	NSString *scheme = url.scheme.lowercaseString;
	BOOL newWindow = action.targetFrame == nil;
	BOOL mainFrame = action.targetFrame.isMainFrame;
	WKNavigationActionPolicy policy = WKNavigationActionPolicyCancel;
	NSString *why;
	if (action.shouldPerformDownload) {
		NSString *own = [NSString stringWithFormat:@"blob:wails://%@/", webView.URL.host];
		BOOL blob = [scheme isEqualToString:@"blob"] && webView.URL.host != nil && [url.absoluteString hasPrefix:own];
		// Unlike navigation, a download has no first-load case, so an unknown page host is refused.
		if ([scheme isEqualToString:@"wails"] && webView.URL.host != nil && sameConnection(webView, url)) {
			// WKDownload cannot load a custom scheme (NSURLErrorDomain -1002), so Go fetches it through the connection's proxy.
			spikeSaveURL((char *)url.absoluteString.UTF8String);
		} else if (blob) {
			policy = WKNavigationActionPolicyDownload;
		}
		why = @"download";
	} else if ([scheme isEqualToString:@"wails"]) {
		if (sameConnection(webView, url)) policy = WKNavigationActionPolicyAllow;
		why = @"wails";
	} else if ([scheme isEqualToString:@"about"] && [url.absoluteString isEqualToString:@"about:blank"]) {
		policy = WKNavigationActionPolicyAllow;
		why = @"about:blank";
	} else if (isWeb(scheme)) {
		// A new window or the main frame leaves for the system browser; a sub-frame is only refused.
		if (mainFrame || newWindow) openOutside(url);
		why = @"web";
	} else {
		why = @"other";
	}
	note(@"navigation %@ (%@, main=%d new=%d) -> %@", url.absoluteString, why, mainFrame, newWindow,
		policy == WKNavigationActionPolicyAllow ? @"allow" : policy == WKNavigationActionPolicyDownload ? @"download" : @"cancel");
	handler(policy);
}

static void decideResponse(id self, SEL _cmd, WKWebView *webView, WKNavigationResponse *navResponse, void (^handler)(WKNavigationResponsePolicy)) {
	NSString *disposition = nil;
	if ([navResponse.response isKindOfClass:[NSHTTPURLResponse class]]) {
		disposition = [((NSHTTPURLResponse *)navResponse.response) valueForHTTPHeaderField:@"Content-Disposition"];
	}
	NSString *kind = [disposition.lowercaseString stringByTrimmingCharactersInSet:NSCharacterSet.whitespaceCharacterSet];
	BOOL download = !navResponse.canShowMIMEType || [kind hasPrefix:@"attachment"];
	note(@"response %@ (%@, disposition=%@) -> %@", navResponse.response.URL, navResponse.response.MIMEType, disposition, download ? @"download" : @"allow");
	handler(download ? WKNavigationResponsePolicyDownload : WKNavigationResponsePolicyAllow);
}

static WKWebView *createWindow(id self, SEL _cmd, WKWebView *webView, WKWebViewConfiguration *configuration, WKNavigationAction *action, WKWindowFeatures *features) {
	NSURL *url = action.request.URL;
	note(@"window.open %@", url.absoluteString);
	if (isWeb(url.scheme.lowercaseString)) openOutside(url);
	return nil;
}

// Returns how many of the five methods were added.
int spikeInstallDelegates(void) {
	Class delegate = objc_getClass("WebviewWindowDelegate");
	if (delegate == nil) return -1;
	downloads = [SpikeDownloads new];
	int added = 0;
	added += class_addMethod(delegate, @selector(webView:decidePolicyForNavigationAction:decisionHandler:), (IMP)decideAction, "v@:@@@?");
	added += class_addMethod(delegate, @selector(webView:decidePolicyForNavigationResponse:decisionHandler:), (IMP)decideResponse, "v@:@@@?");
	added += class_addMethod(delegate, @selector(webView:navigationAction:didBecomeDownload:), (IMP)becameDownload, "v@:@@@");
	added += class_addMethod(delegate, @selector(webView:navigationResponse:didBecomeDownload:), (IMP)becameDownload, "v@:@@@");
	added += class_addMethod(delegate, @selector(webView:createWebViewWithConfiguration:forNavigationAction:windowFeatures:), (IMP)createWindow, "@@:@@@@");
	return added;
}
