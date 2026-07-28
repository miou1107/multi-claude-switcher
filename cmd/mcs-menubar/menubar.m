#import <Cocoa/Cocoa.h>
#import <WebKit/WebKit.h>
#import "menubar.h"

// Implemented in Go.
extern void goPanelWillOpen(void);
extern void goPanelAction(const char *action, const char *folder);
extern void goPanelReady(void);

@interface MCSDelegate : NSObject <WKScriptMessageHandler>
@property (strong) NSStatusItem *item;
@property (strong) NSPopover *popover;
@property (strong) WKWebView *web;
@end

@implementation MCSDelegate

// JS → Go: page calls window.webkit.messageHandlers.mcs.postMessage({action, folder}).
- (void)userContentController:(WKUserContentController *)ucc
      didReceiveScriptMessage:(WKScriptMessage *)message {
  NSDictionary *b = [message.body isKindOfClass:[NSDictionary class]] ? message.body : @{};
  NSString *action = b[@"action"] ?: @"";
  NSString *folder = b[@"folder"] ?: @"";
  goPanelAction(action.UTF8String, folder.UTF8String);
}

- (void)toggle:(id)sender {
  if (self.popover.isShown) { [self.popover performClose:sender]; return; }
  goPanelWillOpen(); // Go renders fresh content and calls LoadPanelHTML
  NSButton *btn = self.item.button;
  [self.popover showRelativeToRect:btn.bounds ofView:btn preferredEdge:NSRectEdgeMaxY];
  [self.popover.contentViewController.view.window makeKeyWindow];
}
@end

static MCSDelegate *gD;

void RunMenuBar(void) {
  @autoreleasepool {
    NSApplication *app = [NSApplication sharedApplication];
    [app setActivationPolicy:NSApplicationActivationPolicyAccessory];

    gD = [[MCSDelegate alloc] init];

    WKWebViewConfiguration *cfg = [[WKWebViewConfiguration alloc] init];
    WKUserContentController *ucc = [[WKUserContentController alloc] init];
    [ucc addScriptMessageHandler:gD name:@"mcs"];
    cfg.userContentController = ucc;
    gD.web = [[WKWebView alloc] initWithFrame:NSMakeRect(0, 0, 400, 540) configuration:cfg];
    gD.web.wantsLayer = YES;

    NSViewController *vc = [[NSViewController alloc] init];
    vc.view = gD.web;

    gD.popover = [[NSPopover alloc] init];
    gD.popover.contentViewController = vc;
    gD.popover.contentSize = NSMakeSize(400, 540);
    gD.popover.behavior = NSPopoverBehaviorTransient;
    gD.popover.animates = YES;

    gD.item = [[NSStatusBar systemStatusBar] statusItemWithLength:NSVariableStatusItemLength];
    gD.item.button.title = @"\U0001F441";
    gD.item.button.target = gD;
    gD.item.button.action = @selector(toggle:);

    goPanelReady(); // load initial content into the (hidden) webview
    [app run];
  }
}

// LoadPanelHTML swaps the popover's webview content (always on the main thread).
void LoadPanelHTML(const char *html) {
  NSString *h = [NSString stringWithUTF8String:html];
  dispatch_async(dispatch_get_main_queue(), ^{
    [gD.web loadHTMLString:h baseURL:nil];
  });
}

void ClosePopover(void) {
  dispatch_async(dispatch_get_main_queue(), ^{ [gD.popover performClose:nil]; });
}

void TerminateApp(void) {
  dispatch_async(dispatch_get_main_queue(), ^{ [NSApp terminate:nil]; });
}
