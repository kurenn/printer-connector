# Spoolr Connect — macOS menubar app (SwiftUI)

Native macOS status-bar app: a 340pt popover for at-a-glance fleet status and
printer pairing. Built from the design handoff (`design_handoff_menubar`) —
SwiftUI per the handoff's explicit direction, **not** a literal HTML/CSS port.

## Build & run

```sh
cd macos/SpoolrConnect
swift build            # compiles the executable
swift run              # launches the menubar app (icon appears in the status bar)
```

Requires the Xcode toolchain (Swift 5.9+, macOS 13+). It runs as an
`.accessory` app — no Dock icon, no main window (equivalent to
`LSUIElement = YES`). Click the status-bar icon to toggle the popover; quit from
the popover's foot strip.

## What's implemented

All five views from the handoff, on a vibrant `NSVisualEffectView` material:

| View | Source | Notes |
| --- | --- | --- |
| **Attention Mode** (home) | `AttentionModeView.swift` | hero counts, errors inline, active prints w/ progress, collapsed Idle/Offline groups |
| **Empty** (first run) | `TransientViews.swift` | pulsing search ripple + CTAs |
| **Scanning** | `TransientViews.swift` | probe strip + discovered rows w/ Pair |
| **Pairing** | `TransientViews.swift` | agent↔printer trio + 4-step checklist |
| **Just paired** | `TransientViews.swift` | success pop + newly-paired row, 6s auto-return |

Plus the design system (`DesignSystem.swift`), shared atoms
(`Components.swift`, `SharedControls.swift`), and the motion specs (pulsing dot,
expanding ripple, scan halo, staggered pair dots, spinner, success pop) — all of
which honour `accessibilityReduceMotion`.

## Architecture

```
App.swift            @main + AppDelegate: NSStatusItem + NSPopover hosting RootView
RootView.swift       vibrancy material + switch over the 5 PopoverStates
Model.swift          FleetModel (ObservableObject) + domain types + the state machine
DesignSystem.swift   color tokens, type, radii, sizing (mirrors the web app)
VisualEffect.swift   NSViewRepresentable vibrancy wrapper
Components.swift     brand mark, status badge, printer row, progress bar, header
SharedControls.swift action rows, foot strip, hero stats, accent button
AttentionModeView.swift / TransientViews.swift   the views
```

## What is stubbed (and where the real agent plugs in)

The connector **agent** (the Go process in this repo) is the source of truth.
This app currently renders the handoff's **sample fleet** (`FleetModel.loadSample()`)
so every state can be built and reviewed standalone.

To wire the live agent: replace `loadSample()` with a subscription to the agent's
status stream (AsyncStream / Combine) and publish into `FleetModel`'s
`@Published` properties — the views react automatically. View selection follows
the handoff state-machine mapping documented in `Model.swift`. The pairing /
scanning transitions here are driven by user intent + short demo timers; in
production they're driven by agent events.

## Deferred (follow-ups, see handoff checklist)

- Package as a real `.app` bundle with `Info.plist` (`LSUIElement`), Geist/Geist
  Mono bundled fonts, and the Spoolr mark as a template PDF (+ coloured variant
  when a print is active). Today it's a SwiftPM executable using SF Pro / SF Mono
  (the handoff-sanctioned fallback) and an SF Symbol tray icon.
- Right-click status-item context-menu fallback; full keyboard map
  (`⌘N/⌘D/⌘,/⌘Q/Esc/↑↓↩`).
- Real agent status stream + pairing protocol wiring.
- Large-fleet scroll cap (≤640pt) tuning + scroll fade mask refinement.
