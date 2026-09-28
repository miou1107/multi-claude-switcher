#ifndef MCS_MENUBAR_H
#define MCS_MENUBAR_H
void RunMenuBar(void);
void LoadPanelHTML(const char *html);
void ClosePopover(void);
void SetPopoverSticky(int sticky);
void SetPopoverHeight(double height);
void TerminateApp(void);
#endif
