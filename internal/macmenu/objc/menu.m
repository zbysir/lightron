// Loaded into the engine's browser process on macOS (DYLD_INSERT_LIBRARIES, app_darwin.go): puts the app's menus
// into the browser's menu bar, gives the Dock icon the app's menu, and takes the Dock icon's clicks. The browser has
// no API for its menus and the Go side is another process, so this is the way in; it can load because the engine is
// a re-signed copy of the browser (no hardened runtime, no library validation; internal/macengine).
//
// The menu comes from the Go side (menu_darwin.go) as JSON lines over a unix socket ($LIGHTRON_MENU_SOCK); a click
// goes back as a line ("click <n>", "action <name>") and runs there. Items whose behaviour is the browser's (Edit's
// copy / paste…, zoom, full screen, developer tools) are copies of the browser's own items, so they keep working.
//
// Links of the app's schemes (Options.Protocols) come here too: the engine copy is their handler (internal/macengine),
// so LaunchServices sends them to this process as Apple events. Running, they go to the Go side ("url <link>");
// started by LaunchServices for one (the app isn't running), this library stops the browser from starting at all and
// starts the shell with the link instead (coldStart).
//
// Built universal (arm64 + x86_64) by `make menulib` into lightronmenu.dylib, which is checked in and embedded, so
// building an app needs no C compiler.
#import <Cocoa/Cocoa.h>
#import <objc/runtime.h>
#include <dlfcn.h>
#include <spawn.h>
#include <stdlib.h>
#include <sys/socket.h>
#include <sys/un.h>
#include <unistd.h>

static int sock = -1;
static NSMenu *ours;          // the menu bar we set
static NSMenu *chromes;       // Chrome's own, the source of the items we copy
static NSArray *spec;         // the last menu the Go side sent
static id target;
static NSMenu *dockMenu;      // the Dock icon's menu (spec role "dock"); nil = only the system's items
static volatile BOOL shellQuitting; // Quit goes to Chrome now, not to the Go side (ourShouldTerminate)
static FILE *dbgf; // $LIGHTRON_MENU_LOG, for debugging (LIGHTRON_DEBUG=1)
#define dbg(...)                                                                  \
  do {                                                                            \
    if (dbgf) {                                                                   \
      fprintf(dbgf, "%s\n", [[NSString stringWithFormat:__VA_ARGS__] UTF8String]); \
      fflush(dbgf);                                                               \
    }                                                                             \
  } while (0)

static void sendLine(NSString *line) {
  if (sock < 0) return;
  NSData *d = [[line stringByAppendingString:@"\n"] dataUsingEncoding:NSUTF8StringEncoding];
  (void)write(sock, d.bytes, d.length);
}

// ---- links of the app's schemes ----
extern char **environ;
static NSMutableArray<NSString *> *pendingURLs; // came before the Go side connected
static NSMutableArray<NSString *> *coldURLs;    // coldStart: what LaunchServices started us for
static NSArray<NSString *> *schemes;            // $LIGHTRON_SCHEMES: this copy is made their default handler

static void stopCold(void) {
  [NSApp stop:nil];
  // stop: takes effect after the next event: post one
  [NSApp postEvent:[NSEvent otherEventWithType:NSEventTypeApplicationDefined location:NSZeroPoint modifierFlags:0
                                     timestamp:0 windowNumber:0 context:nil subtype:0 data1:0 data2:0]
           atStart:YES];
}

@interface LightronURLHandler : NSObject
@end
@implementation LightronURLHandler
- (void)getURL:(NSAppleEventDescriptor *)event withReply:(NSAppleEventDescriptor *)reply {
  NSString *u = [[event paramDescriptorForKeyword:keyDirectObject] stringValue];
  u = [[u componentsSeparatedByCharactersInSet:NSCharacterSet.newlineCharacterSet] componentsJoinedByString:@""];
  if (!u.length) return;
  dbg(@"url %@", u);
  if (coldURLs) {
    [coldURLs addObject:u];
    stopCold();
  } else if (sock >= 0) {
    sendLine([@"url " stringByAppendingString:u]);
  } else {
    [pendingURLs addObject:u];
  }
}
@end

static id urlHandler;
// takeURLs: links come to us, not to Chrome (which would open them as a tab, or hand them back to macOS).
static void takeURLs(void) {
  if (!urlHandler) urlHandler = [LightronURLHandler new];
  [[NSAppleEventManager sharedAppleEventManager] setEventHandler:urlHandler
                                                     andSelector:@selector(getURL:withReply:)
                                                   forEventClass:kInternetEventClass
                                                      andEventID:kAEGetURL];
}

static void sendPendingURLs(void) {
  for (NSString *u in pendingURLs) sendLine([@"url " stringByAppendingString:u]);
  [pendingURLs removeAllObjects];
}

// claimSchemes: this copy is the default handler of the app's schemes (the app's .app doesn't claim them; an older
// install of the app, another copy of the browser, may have).
static void claimSchemes(void) {
  NSString *bid = NSBundle.mainBundle.bundleIdentifier;
  if (!bid) return;
  for (NSString *s in schemes) {
#pragma clang diagnostic push
#pragma clang diagnostic ignored "-Wdeprecated-declarations"
    OSStatus st = LSSetDefaultHandlerForURLScheme((__bridge CFStringRef)s, (__bridge CFStringRef)bid);
#pragma clang diagnostic pop
    dbg(@"default handler of %@: %d", s, (int)st);
  }
}

// coldStart: LaunchServices started the engine for a link (LIGHTRON_COLD in its LSEnvironment) while the app wasn't
// running. Before the browser starts (we are in a library initializer: its main hasn't run), take the link, start the
// shell with it as an argument (the shell starts the engine its own way), and leave. Without a link in 3 s (the
// engine opened by hand), the shell is started anyway.
static void coldStart(void) {
  coldURLs = [NSMutableArray new];
  [NSApplication sharedApplication];
  [NSApp setActivationPolicy:NSApplicationActivationPolicyProhibited];
  takeURLs();
  [NSTimer scheduledTimerWithTimeInterval:3 repeats:NO block:^(NSTimer *t) { stopCold(); }];
  [NSApp run];
  NSString *shell = nil;
  Dl_info info;
  if (dladdr((const void *)coldStart, &info) && info.dli_fname) {
    NSString *dir = [[NSString stringWithUTF8String:info.dli_fname] stringByDeletingLastPathComponent];
    shell = [[NSString stringWithContentsOfFile:[dir stringByAppendingPathComponent:@"shell"] encoding:NSUTF8StringEncoding error:nil]
        stringByTrimmingCharactersInSet:NSCharacterSet.whitespaceAndNewlineCharacterSet];
  }
  dbg(@"cold start: shell %@, links %@", shell, coldURLs);
  if (shell.length) {
    unsetenv("__CFBundleIdentifier"); // LaunchServices' note that we are the engine: not the shell's
    char **argv = calloc(coldURLs.count + 2, sizeof(char *));
    argv[0] = (char *)shell.fileSystemRepresentation;
    for (NSUInteger i = 0; i < coldURLs.count; i++) argv[i + 1] = (char *)coldURLs[i].UTF8String;
    pid_t pid;
    posix_spawn(&pid, argv[0], NULL, NULL, argv, environ);
  }
  _exit(0);
}

@interface LightronMenuTarget : NSObject
@end
@implementation LightronMenuTarget
- (void)run:(NSMenuItem *)item {
  NSString *v = item.representedObject;
  if ([v hasPrefix:@"lightron "]) v = [v substringFromIndex:9];
  sendLine(v);
}
@end

// findItem: Chrome's item with this key equivalent and modifiers, anywhere in its menus.
static NSMenuItem *findItem(NSMenu *menu, NSString *key, NSEventModifierFlags mods) {
  for (NSMenuItem *it in menu.itemArray) {
    if ([it.keyEquivalent isEqualToString:key] && (it.keyEquivalentModifierMask & 0xffff0000) == mods) return it;
    if (it.submenu) {
      NSMenuItem *x = findItem(it.submenu, key, mods);
      if (x) return x;
    }
  }
  return nil;
}

// findAction: Chrome's item with this action (copy:, toggleFullScreen:…), anywhere in its menus.
static NSMenuItem *findAction(NSMenu *menu, SEL action) {
  for (NSMenuItem *it in menu.itemArray) {
    if (it.action == action) return it;
    if (it.submenu) {
      NSMenuItem *x = findAction(it.submenu, action);
      if (x) return x;
    }
  }
  return nil;
}

static NSEventModifierFlags modsOf(NSArray *mods) {
  NSEventModifierFlags m = 0;
  for (NSString *s in mods) {
    if ([s isEqualToString:@"cmd"]) m |= NSEventModifierFlagCommand;
    else if ([s isEqualToString:@"shift"]) m |= NSEventModifierFlagShift;
    else if ([s isEqualToString:@"alt"]) m |= NSEventModifierFlagOption;
    else if ([s isEqualToString:@"ctrl"]) m |= NSEventModifierFlagControl;
  }
  return m;
}

static NSMenu *build(NSString *title, NSArray *items);

static NSMenuItem *buildItem(NSDictionary *d) {
  if ([d[@"separator"] boolValue]) return [NSMenuItem separatorItem];
  NSString *title = d[@"title"] ?: @"";
  NSString *chrome = d[@"chrome"];
  if (chrome) {
    // A copy of one of Chrome's items, found by what it does or by its key; keeps Chrome's action and tag.
    NSMenuItem *src = nil;
    if ([chrome hasSuffix:@":"]) src = findAction(chromes, NSSelectorFromString(chrome));
    else {
      src = findItem(chromes, d[@"key"] ?: @"", modsOf(d[@"mods"]));
      // Zoom in is ⌘+ or ⌘= depending on the Chrome version / keyboard
      if (!src && [d[@"key"] isEqualToString:@"="]) src = findItem(chromes, @"+", modsOf(d[@"mods"]));
      if (!src && [d[@"key"] isEqualToString:@"+"]) src = findItem(chromes, @"=", modsOf(d[@"mods"]));
    }
    if (!src) return nil;
    NSMenuItem *it = [src copy];
    if (title.length) it.title = title;
    return it;
  }
  NSMenuItem *it = [[NSMenuItem alloc] initWithTitle:title action:nil keyEquivalent:d[@"key"] ?: @""];
  it.keyEquivalentModifierMask = modsOf(d[@"mods"]);
  NSString *sel = d[@"sel"];
  if (sel) {
    it.action = NSSelectorFromString(sel); // standard Cocoa actions (copy:, hide:, terminate:…), first responder
  } else if (d[@"send"]) {
    it.action = @selector(run:);
    it.target = target;
    it.representedObject = [@"lightron " stringByAppendingString:d[@"send"]];
  }
  if (d[@"items"]) it.submenu = build(title, d[@"items"]);
  return it;
}

static NSMenu *build(NSString *title, NSArray *items) {
  NSMenu *m = [[NSMenu alloc] initWithTitle:title];
  if (![items isKindOfClass:[NSArray class]]) return m; // JSON null arrives as NSNull
  for (NSDictionary *d in items) {
    NSMenuItem *it = buildItem(d);
    if (it) [m addItem:it];
  }
  return m;
}

// apply edits Chrome's own menu bar instead of replacing it: Chrome looks its items up while it runs (by tag, on window
// activation) and CHECK-crashes when they're gone, which is what setting a menu bar of our own did. So Chrome's
// menus stay, hidden, except Edit and Window (their behaviour is Chrome's and it is the right one); ours go in between.
// Our items are marked (representedObject "lightron…") so a new spec replaces them.
static BOOL isOurs(NSMenuItem *it) {
  return [it.representedObject isKindOfClass:[NSString class]] && [it.representedObject hasPrefix:@"lightron"];
}

static void apply(void) {
  if (!spec || !NSApp) return;
  NSMenu *bar = NSApp.mainMenu;
  dbg(@"apply: bar has %ld items, spec %ld", (long)bar.numberOfItems, (long)spec.count);
  if (!bar || bar.numberOfItems < 2) return;
  if (bar != ours) ours = bar; // a new menu bar from Chrome: start over on it
  chromes = bar;

  // Ours from before: out.
  for (NSMenuItem *it in [bar.itemArray copy])
    if (isOurs(it)) [bar removeItem:it];
  NSMenu *appMenu = [bar itemAtIndex:0].submenu;
  for (NSMenuItem *it in [appMenu.itemArray copy])
    if (isOurs(it)) [appMenu removeItem:it];

  // Chrome's Edit and Window menus, found by what they hold (their titles are localized).
  NSMenuItem *edit = nil, *window = nil;
  for (NSMenuItem *it in bar.itemArray) {
    if (it.submenu && it.submenu == NSApp.windowsMenu) window = it;
    else if (it.submenu && findAction(it.submenu, @selector(copy:))) edit = it;
  }
  // Everything else of Chrome's (File, View, History, Bookmarks, People, Tab, Help, the app menu's items): hidden.
  for (NSMenuItem *it in bar.itemArray)
    if (it != [bar itemAtIndex:0]) it.hidden = (it != edit && it != window);
  for (NSMenuItem *it in appMenu.itemArray) it.hidden = YES;
  // Chrome's Window menu keeps the window items (minimize, zoom, bring to front, the window list AppKit adds) and loses
  // the browser's (downloads, extensions, task manager, show as tab, name window).
  for (NSMenuItem *it in window.submenu.itemArray) {
    SEL a = it.action;
    it.hidden = !(it.isSeparatorItem || a == @selector(performMiniaturize:) || a == @selector(performZoom:) ||
                  a == @selector(arrangeInFront:) || a == @selector(makeKeyAndOrderFront:));
  }

  // The app menu's items come first in the spec; then the bar, in order, "edit" / "window" meaning Chrome's own.
  NSInteger at = 1;
  NSInteger appAt = 0;
  for (NSDictionary *d in spec) {
    NSString *role = d[@"role"];
    if ([role isEqualToString:@"dock"]) {
      dockMenu = [d[@"none"] boolValue] ? nil : build(@"", d[@"items"] ?: @[]);
      continue;
    }
    if ([role isEqualToString:@"app"]) {
      for (NSDictionary *x in d[@"items"] ?: @[]) {
        NSMenuItem *it = buildItem(x);
        if (!it) continue;
        if (!isOurs(it)) it.representedObject = @"lightron"; // a sel / Chrome item: just the mark
        [appMenu insertItem:it atIndex:appAt++];
      }
      continue;
    }
    NSMenuItem *top = nil;
    if ([role isEqualToString:@"edit"]) top = edit;
    else if ([role isEqualToString:@"window"]) top = window;
    if (top) {
      // Chrome's own, moved to its place among ours (the same object: Chrome's references to it stay good).
      [bar removeItem:top];
      if (d[@"title"]) top.title = d[@"title"];
      if (top.submenu && d[@"title"]) top.submenu.title = d[@"title"];
    } else {
      top = [[NSMenuItem alloc] initWithTitle:d[@"title"] ?: @"" action:nil keyEquivalent:@""];
      top.submenu = build(d[@"title"] ?: @"", d[@"items"] ?: @[]);
      top.representedObject = @"lightron";
    }
    [bar insertItem:top atIndex:at++];
  }
  // Hiding items left separators next to each other (or at the ends): hide the extra ones.
  for (NSMenuItem *top in bar.itemArray) {
    BOOL prevSep = YES; // a separator first in the menu is extra too
    NSMenuItem *lastVisible = nil;
    for (NSMenuItem *it in top.submenu.itemArray) {
      if (it.isSeparatorItem) {
        it.hidden = prevSep;
        prevSep = YES;
      } else if (!it.hidden) {
        prevSep = NO;
      }
      if (!it.hidden) lastVisible = it;
    }
    if (lastVisible.isSeparatorItem) lastVisible.hidden = YES;
  }
  if (dbgf) {
    NSMenu *dm = [NSApp.delegate applicationDockMenu:NSApp];
    dbg(@"dock menu: %@", [[dm.itemArray valueForKeyPath:@"title"] componentsJoinedByString:@" / "]);
    NSMutableArray *vis = [NSMutableArray array];
    for (NSMenuItem *it in bar.itemArray)
      if (!it.hidden) [vis addObject:[NSString stringWithFormat:@"%@[%@]", it.title, [[it.submenu.itemArray filteredArrayUsingPredicate:[NSPredicate predicateWithFormat:@"hidden == NO"]] valueForKeyPath:@"title"]]];
    dbg(@"visible: %@", [vis componentsJoinedByString:@" | "]);
  }
}

static void readLoop(void) {
  NSMutableData *buf = [NSMutableData data];
  char chunk[65536];
  for (;;) {
    ssize_t n = read(sock, chunk, sizeof chunk);
    if (n <= 0) break;
    [buf appendBytes:chunk length:n];
    for (;;) {
      const char *b = buf.bytes;
      const char *nl = memchr(b, '\n', buf.length);
      if (!nl) break;
      NSData *line = [buf subdataWithRange:NSMakeRange(0, nl - b)];
      [buf replaceBytesInRange:NSMakeRange(0, nl - b + 1) withBytes:NULL length:0];
      id v = [NSJSONSerialization JSONObjectWithData:line options:0 error:nil];
      dbg(@"got a line of %lu bytes", (unsigned long)line.length);
      NSString *text = [[NSString alloc] initWithData:line encoding:NSUTF8StringEncoding];
      if ([text isEqualToString:@"quitting"]) { // the Go side is closing the browser: let Quit through (ourShouldTerminate)
        shellQuitting = YES;
        // Out of the Dock and ⌘Tab now: Chrome's shutdown (saving its state) can take seconds, and an icon still there
        // looks like the app didn't quit (a click on it would only ask for a window that isn't coming)
        dispatch_async(dispatch_get_main_queue(), ^{
          [NSApp setActivationPolicy:NSApplicationActivationPolicyProhibited];
        });
        continue;
      }
      if ([text isEqualToString:@"activate"]) {
        // The Go side bringing the app forward (a notification clicked, the app opened again). From inside: `open`
        // on the app sends it a reopen, and Chrome answers a reopen with a new-tab window.
        dispatch_async(dispatch_get_main_queue(), ^{
          if (@available(macOS 14.0, *)) [NSApp activate];
          else [NSApp activateIgnoringOtherApps:YES];
        });
        continue;
      }
      if ([v isKindOfClass:[NSArray class]]) {
        dispatch_async(dispatch_get_main_queue(), ^{
          spec = v;
          apply();
        });
      }
    }
  }
  close(sock);
  sock = -1;
  // The Go side is gone (quit, crashed, killed): nobody drives this browser any more, and an app in the Dock that
  // nothing answers for is worse than none. Quit with it.
  dbg(@"shell gone: quitting");
  dispatch_async(dispatch_get_main_queue(), ^{
    [NSApp terminate:nil];
  });
}

static void connectShell(NSString *path) {
  dispatch_async(dispatch_get_global_queue(QOS_CLASS_UTILITY, 0), ^{
    for (int i = 0; i < 100 && sock < 0; i++) {
      int fd = socket(AF_UNIX, SOCK_STREAM, 0);
      struct sockaddr_un a = {.sun_family = AF_UNIX};
      strlcpy(a.sun_path, path.fileSystemRepresentation, sizeof a.sun_path);
      if (connect(fd, (struct sockaddr *)&a, sizeof a) == 0) {
        dbg(@"connected");
        sock = fd;
        dispatch_async(dispatch_get_main_queue(), ^{
          sendPendingURLs();
        });
        break;
      }
      close(fd);
      usleep(200 * 1000);
    }
    if (sock >= 0) {
      readLoop();
      return;
    }
    // The Go side never answered (it died before we were up): nobody will drive or close this browser. Quit.
    dbg(@"no shell: quitting");
    dispatch_async(dispatch_get_main_queue(), ^{
      [NSApp terminate:nil];
    });
  });
}

// The Dock icon's menu comes from the app delegate's applicationDockMenu:, where Chrome answers New Window / New
// Incognito Window (and profiles). Ours answers instead; AppKit adds the window list, Options, Hide and Quit itself.
static NSMenu *ourDockMenu(id self, SEL _cmd, NSApplication *app) {
  return dockMenu;
}

// A click on the Dock icon is a "reopen": the delegate's applicationShouldHandleReopen:hasVisibleWindows:, where Chrome
// opens a new-tab window when it has no browser window (our --app windows don't count). The Go side closed that window
// and brought ours up, but it showed for a moment: a box flashing on every click. Ours tells the Go side instead (it
// brings a window up, or opens one) and has AppKit do nothing more.
static BOOL ourReopen(id self, SEL _cmd, NSApplication *app, BOOL visible) {
  sendLine(@"action reopen");
  return NO;
}

static void takeReopen(void) {
  Class c = [NSApp.delegate class];
  if (!c) return;
  SEL sel = @selector(applicationShouldHandleReopen:hasVisibleWindows:);
  Method m = class_getInstanceMethod(c, sel);
  if (m) method_setImplementation(m, (IMP)ourReopen);
  else class_addMethod(c, sel, (IMP)ourReopen, "c@:@c");
  dbg(@"reopen taken from %@", NSStringFromClass(c));
}

// Quit (⌘Q, the Dock menu's Quit, a quit Apple event) reaches the delegate's applicationShouldTerminate:. Chrome then
// closes its windows, but its process stays up (it was started for a debugging pipe, with no startup window), so the
// Go side saw only "no window left" — with KeepRunning, the app stayed in the Dock with nothing to show. Ours tells the
// Go side to quit instead (it closes the browser over CDP). The Go side says "quitting" when it closes the browser
// itself, and with no Go side at all Chrome's own answer goes through: Chrome's shutdown path may come back here.
static IMP chromeShouldTerminate;
static NSApplicationTerminateReply ourShouldTerminate(id self, SEL _cmd, NSApplication *app) {
  if (sock >= 0 && !shellQuitting) {
    shellQuitting = YES; // a second Quit before the Go side closes the browser goes to Chrome
    dbg(@"quit: asking the shell");
    sendLine(@"action quit");
    return NSTerminateCancel;
  }
  if (chromeShouldTerminate) return ((NSApplicationTerminateReply(*)(id, SEL, NSApplication *))chromeShouldTerminate)(self, _cmd, app);
  return NSTerminateNow;
}

static void takeTerminate(void) {
  Class c = [NSApp.delegate class];
  if (!c) return;
  SEL sel = @selector(applicationShouldTerminate:);
  Method m = class_getInstanceMethod(c, sel);
  if (m) chromeShouldTerminate = method_setImplementation(m, (IMP)ourShouldTerminate);
  else class_addMethod(c, sel, (IMP)ourShouldTerminate, "Q@:@");
  dbg(@"terminate taken from %@", NSStringFromClass(c));
}

static void takeDockMenu(void) {
  Class c = [NSApp.delegate class];
  if (!c) return;
  SEL sel = @selector(applicationDockMenu:);
  Method m = class_getInstanceMethod(c, sel);
  if (m) method_setImplementation(m, (IMP)ourDockMenu);
  else class_addMethod(c, sel, (IMP)ourDockMenu, "@@:@");
  dbg(@"dock menu taken from %@", NSStringFromClass(c));
}

__attribute__((constructor)) static void start(void) {
  const char *p = getenv("LIGHTRON_MENU_SOCK");
  BOOL cold = getenv("LIGHTRON_COLD") != NULL; // from the copy's LSEnvironment: started by LaunchServices
  const char *sch = getenv("LIGHTRON_SCHEMES");
  // Not for the processes Chrome starts: its helpers are Google-signed with library validation, and would be killed
  // for loading this ad hoc signed library.
  unsetenv("DYLD_INSERT_LIBRARIES");
  unsetenv("LIGHTRON_COLD");
  unsetenv("LIGHTRON_SCHEMES");
  if (getenv("LIGHTRON_MENU_LOG")) dbgf = fopen(getenv("LIGHTRON_MENU_LOG"), "a");
  unsetenv("LIGHTRON_MENU_LOG");
  if (!p) {
    if (cold) coldStart();
    return;
  }
  NSString *path = [NSString stringWithUTF8String:p];
  dbg(@"loaded");
  unsetenv("LIGHTRON_MENU_SOCK");
  pendingURLs = [NSMutableArray new];
  if (sch && *sch) schemes = [[NSString stringWithUTF8String:sch] componentsSeparatedByString:@","];
  target = [LightronMenuTarget new];
  [[NSNotificationCenter defaultCenter] addObserverForName:NSApplicationDidFinishLaunchingNotification object:nil queue:nil
                                                usingBlock:^(NSNotification *n) {
                                                  dbg(@"did finish launching");
                                                  takeDockMenu();
                                                  takeReopen();
                                                  takeTerminate();
                                                  if (schemes.count) {
                                                    takeURLs();
                                                    claimSchemes();
                                                    // Chrome may set its own handler later: take it back once more
                                                    dispatch_after(dispatch_time(DISPATCH_TIME_NOW, 2 * NSEC_PER_SEC),
                                                                   dispatch_get_main_queue(), ^{
                                                                     takeURLs();
                                                                   });
                                                  }
                                                  connectShell(path);
                                                  // Chrome may set its menu again later (a locale or profile change):
                                                  // put ours back.
                                                  [NSTimer scheduledTimerWithTimeInterval:1 repeats:YES block:^(NSTimer *t) {
                                                    if (spec && NSApp.mainMenu != ours) apply();
                                                  }];
                                                }];
}
