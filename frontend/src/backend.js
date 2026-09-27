// The one module that imports Wails' generated bindings and JS runtime -
// main.js talks to the Go backend only through what this file exports. A
// Wails major-version move (v3 generates bindings elsewhere and replaces
// wailsjs/runtime with the @wailsio/runtime package, whose event callbacks
// receive an event object rather than the bare payload) is then a change to
// this file alone, not to every call site in main.js.

// Namespace import, not named imports - deliberate. Wails regenerates
// wailsjs/go/main/App.js from whatever the *App struct's method set actually
// is FOR THE PLATFORM IT WAS LAST BUILT FOR, and a fair number of App's own
// methods only exist on Windows (Spooler/Flash Drive/DEVMODE capture/Driver-
// combobox methods/app self-update/7-Zip - see app.go's own "explicitly out
// of scope" notes). A named import of a function that doesn't exist in the
// generated module is a hard build-time failure under Vite/Rollup (confirmed
// live building this exact file against a darwin-generated App.js); a
// namespace import sidesteps that entirely - App.SomeWindowsOnlyMethod is
// simply undefined at runtime on a macOS build, and every call site in
// main.js that can reach one is already guarded by isMac().
import * as App from '../wailsjs/go/main/App';
import {EventsOn} from '../wailsjs/runtime/runtime';

export {App};

// onEvent registers handler for a backend event (the Go side's a.emit) and
// calls it with the event's payload.
export function onEvent(name, handler) {
    return EventsOn(name, handler);
}
