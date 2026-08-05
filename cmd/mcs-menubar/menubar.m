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

// SetPopoverSticky keeps the panel on screen while an operation the user needs
// to see through is running.
//
// The panel is Transient by default, which means it closes the moment anything
// outside it takes focus. That is right for browsing: click elsewhere and it
// gets out of the way. It is wrong for a switch, because a switch ENDS by
// launching Claude Desktop, and Claude taking the foreground is exactly the
// event that closes the panel. So the card reporting the outcome was dismissed
// by the very thing it was reporting, and a switch that failed said so to an
// empty screen.
//
// ApplicationDefined means only we close it. ClosePopover above still works, so
// Escape and the menu bar icon remain a way out; nothing here can trap the user
// behind a panel that will not go away.
void SetPopoverSticky(int sticky) {
  dispatch_async(dispatch_get_main_queue(), ^{
    gD.popover.behavior = sticky ? NSPopoverBehaviorApplicationDefined
                                 : NSPopoverBehaviorTransient;
  });
}

void TerminateApp(void) {
  dispatch_async(dispatch_get_main_queue(), ^{ [NSApp terminate:nil]; });
}
