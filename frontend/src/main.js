import './style.css';
import './app.css';

// Namespace import, not named imports - deliberate. Wails regenerates
// wailsjs/go/main/App.js from whatever the *App struct's method set actually
// is FOR THE PLATFORM IT WAS LAST BUILT FOR, and a fair number of App's own
// methods only exist on Windows (Spooler/Flash Drive/DEVMODE capture/Driver-
// combobox methods/app self-update/7-Zip - see app.go's own "explicitly out
// of scope" notes). A named import of a function that doesn't exist in the
// generated module is a hard build-time failure under Vite/Rollup (confirmed
// live building this exact file against a darwin-generated App.js); a
// namespace import sidesteps that entirely - App.SomeWindowsOnlyMethod is
// simply undefined at runtime on a macOS build, and every call site below
// that can reach one is already guarded by isMac().
import * as App from '../wailsjs/go/main/App';
import {EventsOn} from '../wailsjs/runtime/runtime';

// Tooltip text, shared between the static template below and the
// dynamically-generated per-row HTML (rowHtml/mfgSelectHtml), so every
// control - defaults, grid headers, and each row's own fields - carries the
// same explanation.
const TIP = {
    salesChainId: 'Used to name saved configuration/DEVMODE files for this job. Letters, numbers, hyphen, and underscore only.',
    manufacturer: 'Printer manufacturer - determines which drivers are offered.',
    driver: 'Driver to install/use for this printer. Type to fuzzy-search; multiple local versions of the same driver appear as separate dated entries - for a model-specific driver name (Kyocera, mainly), typing part of the model narrows the list the same way.',
    model: 'Printer model - used to pick the right PPD when the manufacturer has multiple local driver packages, or to fuzzy-match a fallback PPD when no manufacturer package is installed. Optional when a manufacturer has just one installer package.',
    subnet: 'Pre-fills new rows\' IP with this subnet (a trailing "." is added automatically if you don\'t type one) - e.g. "10.1.1." so you only need to type the last octet per row.',
    portPrefixEnabled: 'When creating a new Standard TCP/IP port, prefix its name with the text to the right instead of using the bare IP address.',
    portPrefixText: 'Prefix text used when Port name prefix is checked, e.g. "IP_" - the port would be named "IP_10.1.1.50".',
    useExistingPort: 'Reuse the Standard TCP/IP port already configured for this row\'s IP instead of creating a new one. Falls back to creating a new port if none already targets the IP.',
    snmp: 'Enable SNMP status monitoring on new rows\' ports by default (only applies when a new port is actually created) - the community string to the right is used when checked.',
    snmpCommunity: 'SNMP community string used on new rows\' ports when SNMP is checked - defaults to "public". Disabled until SNMP is checked.',
    snmpGrid: 'This row\'s SNMP community string - blank disables SNMP monitoring on this row\'s port; any text enables it and is the community string used (only applies when a new port is actually created).',
    mono: 'Deploy this printer set to monochrome (black & white) printing by default.',
    oneSided: 'Deploy this printer set to simplex (single-sided) printing by default.',
    apf: 'Enable "Advanced printing features" on the printer\'s Advanced tab.',
    select: 'Included in the next deploy.',
    name: 'Printer object name.',
    ip: 'Printer\'s IP address, or "NUL" to bind permanently to the local NUL: port.',
    selectAllHeader: 'Check/uncheck every row.',
    saveFileBasePath: 'Where PDT keeps saved JSON configs and captured DEVMODE/Device Settings files, and where Open/Save Configuration start from by default. Defaults to Configs\\ on this flash drive when running portably, or %LocalAppData%\\PDT\\Configs for an installed copy.',
    driversBasePath: 'Where PDT looks for printer drivers (Drivers\\Windows\\<version>\\<Manufacturer>\\...) - click Refresh (or restart PDT) after changing this to rescan the new location. Defaults to Drivers\\ on this flash drive when running portably, or %LocalAppData%\\PDT\\Drivers for an installed copy.',
    preinstallBasePath: 'Where site-survey "<SaveID> - <Client> - <Address>" subfolders live - Export Configs looks here for the one matching the current Save ID.',
    manufacturerOrder: 'Drag to reorder - controls the Manufacturer dropdown\'s order in Defaults and in the grid. Settings > External Sites is always alphabetical regardless of this order.',
};

// HTML-attribute-escapes a string for use inside title="..." - every tooltip
// above is plain JS text (some containing literal " and & - e.g.
// `"SalesChain: <id>"`), and every use of TIP below embeds it inside a
// double-quoted attribute, so this must run first or an embedded " would
// close the attribute early and leak the rest of the tooltip as visible page
// text. Builds on attr() (defined further down, used for value="..."
// attributes) plus </> escaping, which value="..." doesn't strictly need but
// a title="..." full sentence might plausibly contain.
function tip(key) {
    return attr(TIP[key]).replace(/</g, '&lt;').replace(/>/g, '&gt;');
}

// One row's canonical shape in this frontend - a single camelCase-ish shape
// used everywhere in JS, converted to each wire DTO's own (and mutually
// inconsistent - printer.PrinterRow uses "SNMP", config.SavedRow uses "Snmp")
// field-name casing only at the boundary calls below, so the rest of this
// file never has to think about it. Every row gets a stable, never-reused
// _id so the grid (and, crucially, live deploy-progress updates) can find a
// specific row's <tr> without relying on its position in state.rows, which
// shifts as rows are added/removed.
let nextRowId = 1;

function newRow(overrides = {}) {
    return Object.assign({
        _id: nextRowId++,
        select: true, name: '', ip: '', manufacturer: '', model: '', driver: '',
        // '' disables SNMP on this row's port; any other text enables it and
        // is the community string used (only applies when a new port is
        // actually created) - see tip('snmpGrid').
        snmpCommunity: '',
        mono: false, oneSided: false, useExistingPort: false, advancedPrintingFeatures: false,
        // Bare filename under the Configs folder (e.g. "18455-1-Copy Room.bin")
        // pointing at a captured/browsed raw DEVMODE - never the DEVMODE
        // bytes themselves, which never travel through this frontend at all.
        // '' means none explicitly assigned (Deploy still checks the Configs
        // folder by convention as a fallback in that case).
        devModeFile: '',
    }, overrides);
}

// A pre-existing saved config from before the SNMP community field existed
// only ever had a bare Snmp bool, no community string - "public" (matching
// AddStandardTcpIpPort's own default for an empty community) preserves that
// file's original behavior instead of silently turning SNMP off for it.
function inferSnmpCommunity(snmpEnabled, community) {
    if (community) return community;
    return snmpEnabled ? 'public' : '';
}

function rowToPrinterRow(r) {
    return {
        Name: r.name, IP: r.ip, Manufacturer: r.manufacturer, Model: r.model, Driver: r.driver,
        SNMP: !!r.snmpCommunity, SNMPCommunity: r.snmpCommunity, Mono: r.mono, OneSided: r.oneSided,
        UseExistingPort: r.useExistingPort, AdvancedPrintingFeatures: r.advancedPrintingFeatures,
        DevModeFile: r.devModeFile,
    };
}

function printerRowToRow(pr, select = true) {
    return newRow({
        select, name: pr.Name, ip: pr.IP, manufacturer: pr.Manufacturer, model: pr.Model, driver: pr.Driver,
        snmpCommunity: inferSnmpCommunity(pr.SNMP, pr.SNMPCommunity), mono: pr.Mono, oneSided: pr.OneSided,
        useExistingPort: pr.UseExistingPort, advancedPrintingFeatures: pr.AdvancedPrintingFeatures,
        devModeFile: pr.DevModeFile || '',
    });
}

function rowToSavedRow(r) {
    return {
        Select: r.select, Name: r.name, IP: r.ip, Manufacturer: r.manufacturer, Model: r.model, Driver: r.driver,
        Snmp: !!r.snmpCommunity, SnmpCommunity: r.snmpCommunity, Mono: r.mono, OneSided: r.oneSided,
        UseExistingPort: r.useExistingPort, AdvancedPrintingFeatures: r.advancedPrintingFeatures,
        DevModeFile: r.devModeFile,
    };
}

function savedRowToRow(sr) {
    return newRow({
        select: sr.Select, name: sr.Name, ip: sr.IP, manufacturer: sr.Manufacturer, model: sr.Model, driver: sr.Driver,
        snmpCommunity: inferSnmpCommunity(sr.Snmp, sr.SnmpCommunity), mono: sr.Mono, oneSided: sr.OneSided,
        useExistingPort: sr.UseExistingPort, advancedPrintingFeatures: sr.AdvancedPrintingFeatures,
        devModeFile: sr.DevModeFile || '',
    });
}

const state = {
    // 'windows' or 'darwin' - set once at the top of init() from the Go
    // side's own runtime.GOOS (App.Platform), before anything else runs.
    // The one feature-detection signal every platform-specific bit of UI
    // below keys off, via the "platform-windows"/"platform-darwin" class
    // isPlatform() adds to <body> - see app.css for the actual hide rules.
    platform: 'windows',
    salesChainId: '',
    portPrefixEnabled: false,
    portPrefixText: '',
    manufacturers: [],
    rows: [],
    deploying: false,
    logLines: [],
    settings: {saveFileBasePath: '', driversBasePath: '', manufacturerUrls: {}, manufacturerOrder: []},
    // Set while Deploy is running: the exact rows submitted, in submission
    // order, plus how many deploy-progress events have arrived so far - since
    // events arrive in that same order, this correlates each event to its
    // row without depending on row Name being unique.
    activeDeploy: null,
};

document.querySelector('#app').innerHTML = `
  <div class="startup-overlay" id="startupOverlay">
    <div class="startup-spinner"></div>
    <div>Initializing...</div>
  </div>
  <div class="top-bar">
    <label title="${tip('salesChainId')}">Save ID <input type="text" id="salesChainId" class="input-needs-value" size="14" title="${tip('salesChainId')}"></label>
    <button id="btnOpenConfig" title="Load a previously saved JSON configuration (rows + Save ID).">Open Configuration</button>
    <button id="btnSaveConfig" title="Save the current rows and Save ID to a JSON configuration file.">Save Configuration</button>
    <button id="btnResetConfig" title="Reset PDT to its default settings - clears every row, the Save ID, and the Defaults panel.">Reset Configuration</button>
    <button id="btnExportConfigs" title="Copy this Save ID's Configs files (saved JSON config, captured DEVMODE/Device Settings) to its Preinstall subfolder on this computer.">Export Configs</button>
    <div class="dropdown platform-windows-only" id="spoolerDropdown">
      <button id="btnSpooler" title="Control the Windows Print Spooler service.">Spooler &#9662;</button>
      <div class="dropdown-menu" id="spoolerMenu" hidden>
        <button type="button" class="dropdown-item" data-spooler-action="restart">Restart</button>
        <button type="button" class="dropdown-item" data-spooler-action="start">Start</button>
        <button type="button" class="dropdown-item" data-spooler-action="stop">Stop</button>
      </div>
    </div>
    <button id="btnFlashDrive" class="icon-btn-inline" title="Write a portable copy of PDT (this executable, Drivers, and Configs) to one or more USB flash drives.">
      <svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round" style="vertical-align: middle;">
        <line x1="12" y1="12" x2="6" y2="7"/>
        <circle cx="6" cy="6" r="1.6"/>
        <line x1="12" y1="12" x2="12" y2="4"/>
        <rect x="10.4" y="2.4" width="3.2" height="3.2"/>
        <line x1="12" y1="12" x2="18" y2="7"/>
        <polygon points="18,4.8 19.8,7.6 16.2,7.6"/>
        <line x1="12" y1="12" x2="12" y2="18"/>
        <polyline points="9.5,16 12,19 14.5,16"/>
      </svg>
    </button>
    <button id="btnRefreshDrivers" class="icon-btn-inline" title="Rescan the Drivers folder for newly added or extracted driver packages, without restarting PDT.">&#128260;</button>
    <button id="btnSyncFlashDrive" class="icon-btn-inline" title="Sync this computer's Drivers folder onto a flash drive that already has a portable PDT copy on it, then extract anything newly-copied.">
      <svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round" style="vertical-align: middle;">
        <line x1="3" y1="8" x2="19" y2="8"/>
        <polyline points="15,4 19,8 15,12"/>
        <line x1="21" y1="16" x2="5" y2="16"/>
        <polyline points="9,12 5,16 9,20"/>
      </svg>
    </button>
    <button id="btnOpenDriversFolder" class="icon-btn-inline" title="Open the Drivers folder in File Explorer.">&#128194;</button>
    <span class="catalog-warning" id="catalogWarning" hidden></span>
    <button id="btnSettings" class="icon-btn" title="Settings">&#9881;</button>
  </div>

  <div class="modal-backdrop" id="settingsBackdrop" hidden>
    <div class="modal settings-modal">
      <h3>Settings</h3>
      <div class="tabs">
        <button type="button" class="tab-btn active" data-tab="general">General</button>
        <button type="button" class="tab-btn" data-tab="sites">External Sites</button>
        <button type="button" class="tab-btn" data-tab="about">About</button>
      </div>
      <div class="tab-panel" data-tab-panel="general">
        <label class="modal-field" title="${tip('saveFileBasePath')}">
          Configuration Files Base Path
          <div class="path-row">
            <input type="text" id="settingsBasePath" title="${tip('saveFileBasePath')}">
            <button id="btnBrowseBasePath" title="Browse for a folder...">&hellip;</button>
          </div>
        </label>
        <label class="modal-field" title="${tip('driversBasePath')}">
          Drivers Base Path
          <div class="path-row">
            <input type="text" id="settingsDriversBasePath" title="${tip('driversBasePath')}">
            <button id="btnBrowseDriversBasePath" title="Browse for a folder...">&hellip;</button>
          </div>
        </label>
        <label class="modal-field" title="${tip('preinstallBasePath')}">
          Preinstall Base Path
          <div class="path-row">
            <input type="text" id="settingsPreinstallBasePath" title="${tip('preinstallBasePath')}">
            <button id="btnBrowsePreinstallBasePath" title="Browse for a folder...">&hellip;</button>
          </div>
        </label>
        <div class="modal-field" title="${tip('manufacturerOrder')}">
          <span>Manufacturer sort order (<a href="#" id="btnAlphabetizeMfgOrder" class="inline-link" title="Sort the list below A-Z.">Alphabetize</a>)</span>
          <ul id="mfgOrderList" class="mfg-order-list" title="${tip('manufacturerOrder')}"></ul>
        </div>
      </div>
      <div class="tab-panel" data-tab-panel="sites" hidden>
        <p class="modal-hint">Pages to check for driver updates - no vendor offers a way to check
          automatically, so "Check for Updates" in Defaults just opens the page below for the
          selected manufacturer.</p>
        <div id="settingsSitesPanel"></div>
      </div>
      <div class="tab-panel" data-tab-panel="about" hidden>
        <div class="about-panel">
          <div class="about-row"><span class="about-label">Name</span><span id="aboutName"></span></div>
          <div class="about-row"><span class="about-label">Version</span><span id="aboutVersion"></span></div>
          <div class="about-row"><span class="about-label">Author</span><span id="aboutAuthor"></span></div>
          <div class="about-row"><span class="about-label">GitHub</span>
            <a href="#" id="aboutRepoLink" title="Open in your browser"></a>
          </div>
        </div>
        <div class="about-update platform-windows-only">
          <button type="button" id="btnCheckUpdate" title="Check this project's GitHub Releases for a newer version.">Check for Updates</button>
          <button type="button" class="primary" id="btnApplyUpdate" hidden title="Download and install the update, then relaunch.">Update Now</button>
          <span class="modal-hint" id="updateStatus"></span>
        </div>
        <p class="modal-hint platform-windows-only">Self-extracting driver packages (Lexmark's own) are unpacked using
          <a href="#" id="sevenZipCreditLink" title="Open 7-zip.org in your browser">7-Zip</a>, by Igor
          Pavlov, bundled with PDT under its own license.</p>
        <div class="about-panel platform-windows-only">
          <div class="about-row"><span class="about-label">7-Zip</span><span id="aboutSevenZipVersion"></span></div>
        </div>
        <div class="about-update platform-windows-only">
          <button type="button" id="btnCheckSevenZipUpdate" title="Check 7-Zip's own releases for a newer version.">Check for 7-Zip Updates</button>
          <button type="button" class="primary" id="btnApplySevenZipUpdate" hidden title="Download and install the update.">Update 7-Zip Now</button>
          <span class="modal-hint" id="sevenZipUpdateStatus"></span>
        </div>
      </div>
      <div class="modal-actions">
        <button id="btnSettingsCancel">Cancel</button>
        <button class="primary" id="btnSettingsSave">Save</button>
      </div>
    </div>
  </div>

  <div class="modal-backdrop" id="importPrintersBackdrop" hidden>
    <div class="modal">
      <h3>Import Printers</h3>
      <p class="modal-hint">Printers already installed on this computer. Physical printers are
        checked by default; software printers (PDF, XPS, fax, etc.) are not - override either as
        needed.</p>
      <div id="importPrintersList" class="import-printers-list"></div>
      <label class="modal-field-inline" title="Immediately capture each imported printer's current DEVMODE (print defaults) and Device Settings after import.">
        <input type="checkbox" id="importPrintersGetDevmode" checked> Get DEVMODE
      </label>
      <div class="modal-actions">
        <button id="btnImportPrintersCancel">Cancel</button>
        <button class="primary" id="btnImportPrintersConfirm">Import</button>
      </div>
    </div>
  </div>

  <div class="modal-backdrop" id="exportFolderBackdrop" hidden>
    <div class="modal">
      <h3>Select Preinstall Folder</h3>
      <p class="modal-hint">More than one Preinstall subfolder matches this Save ID. Choose which one to export to.</p>
      <div id="exportFolderList" class="import-printers-list"></div>
      <div class="modal-actions">
        <button id="btnExportFolderCancel">Cancel</button>
        <button class="primary" id="btnExportFolderConfirm">Select</button>
      </div>
    </div>
  </div>

  <div class="modal-backdrop" id="exportCollisionBackdrop" hidden>
    <div class="modal">
      <h3>Files Already Exist</h3>
      <p class="modal-hint" id="exportCollisionHint"></p>
      <div id="exportCollisionList" class="import-printers-list"></div>
      <div class="modal-actions">
        <button id="btnExportCollisionCancel">Cancel</button>
        <button id="btnExportCollisionNew">Save to New Subfolder</button>
        <button class="primary" id="btnExportCollisionOverwrite">Overwrite</button>
      </div>
    </div>
  </div>

  <div class="modal-backdrop" id="flashDriveBackdrop" hidden>
    <div class="modal">
      <h3 id="flashDriveTitle">Write to Flash Drive</h3>
      <p class="modal-hint" id="flashDriveHint">Writes a portable copy of PDT (this executable, plus its
        Drivers and Configs folders) to every checked drive.</p>
      <div id="flashDriveList" class="import-printers-list"></div>
      <label class="modal-field-inline" id="flashDriveFormatRow" title="Erases all data on every checked drive and lays down a fresh exFAT filesystem before writing PDT to it.">
        <input type="checkbox" id="flashDriveFormat"> Format as exFAT first (erases all data on the selected drive(s))
      </label>
      <div class="modal-actions">
        <button id="btnFlashDriveCancel">Cancel</button>
        <button class="primary" id="btnFlashDriveConfirm">Write</button>
      </div>
    </div>
  </div>

  <div class="modal-backdrop" id="flashCopyProgressBackdrop" hidden>
    <div class="modal">
      <h3 id="flashCopyProgressTitle">Copying...</h3>
      <div id="flashCopyProgressList"></div>
    </div>
  </div>

  <div class="modal-backdrop" id="confirmBackdrop" hidden>
    <div class="modal">
      <h3 id="confirmTitle"></h3>
      <p class="modal-hint" id="confirmMessage"></p>
      <div class="modal-actions">
        <button id="btnConfirmCancel">Cancel</button>
        <button class="primary" id="btnConfirmOk">OK</button>
      </div>
    </div>
  </div>

  <div class="modal-backdrop" id="stopBackdrop" hidden>
    <div class="modal">
      <h3>Warning</h3>
      <p class="modal-hint">Stopping mid-deploy can leave print objects and drivers in an inconsistent
        state, making them difficult or impossible to cleanly remove afterward.</p>
      <p class="modal-hint">Stop finishes whatever row is currently in progress, then starts no further
        row.</p>
      <p class="modal-hint">Force Stop is for when PDT is locked up and Stop doesn't respond: it
        immediately closes PDT itself, abandoning whatever was mid-flight - there is no safer way to
        interrupt a single stuck operation.</p>
      <div class="modal-actions">
        <button id="btnStopCancel">Cancel</button>
        <button class="danger" id="btnStopForce">Force Stop (Closes PDT)</button>
        <button class="primary" id="btnStopGraceful">Stop</button>
      </div>
    </div>
  </div>

  <div class="no-drivers-banner" id="noDriversBanner" hidden>
    No printer drivers are installed yet. Pick a Manufacturer below, then click <strong>Check for Updates</strong> to open its download page - once a driver package is downloaded into the Drivers folder, press the Refresh button (&#128260;) to make it available.
  </div>

  <div class="defaults-panel">
    <fieldset class="defaults-outer">
      <legend>Defaults (used by "Add Printer")</legend>
      <div class="defaults-row">
        <label title="${tip('manufacturer')}">Manufacturer <select id="defMfg" title="${tip('manufacturer')}"></select></label>
        <button type="button" id="btnCheckUpdates" title="Open the selected manufacturer's driver page (configured in Settings &gt; External Sites).">Check for Updates</button>
        <label class="driver-label" title="${tip('driver')}">Driver <div class="combo"><input type="text" id="defDriver" title="${tip('driver')}"><div class="combo-list" id="defDriverList" hidden></div></div></label>
      </div>
      <fieldset class="defaults-sub">
        <legend>Port</legend>
        <label title="${tip('subnet')}">Subnet <input type="text" id="defSubnet" placeholder="10.1.1." title="${tip('subnet')}"></label>
        <label class="platform-windows-only" title="${tip('portPrefixEnabled')}"><input type="checkbox" id="portPrefixEnabled" title="${tip('portPrefixEnabled')}"> Port name prefix</label>
        <input type="text" id="portPrefixText" size="6" placeholder="IP_" class="platform-windows-only" title="${tip('portPrefixText')}">
        <label class="platform-windows-only" title="${tip('useExistingPort')}"><input type="checkbox" id="defUseExistingPort" title="${tip('useExistingPort')}"> Use existing port</label>
        <label class="platform-windows-only" title="${tip('snmp')}"><input type="checkbox" id="defSnmp" title="${tip('snmp')}"> SNMP</label>
        <input type="text" id="defSnmpCommunity" size="8" placeholder="public" class="platform-windows-only" title="${tip('snmpCommunity')}">
      </fieldset>
      <fieldset class="defaults-sub">
        <legend>Print Defaults</legend>
        <label title="${tip('mono')}"><input type="checkbox" id="defMono" checked title="${tip('mono')}"> Monochrome</label>
        <label title="${tip('oneSided')}"><input type="checkbox" id="defOneSided" checked title="${tip('oneSided')}"> 1-sided</label>
      </fieldset>
      <fieldset class="defaults-sub platform-windows-only">
        <legend>Advanced</legend>
        <label title="${tip('apf')}"><input type="checkbox" id="defApf" title="${tip('apf')}"> Enable APF</label>
      </fieldset>
    </fieldset>
  </div>

  <div class="toolbar">
    <button id="btnAddRow" title="Add a new row using the Defaults above.">Add Printer</button>
    <button id="btnRemoveRow" title="Remove every checked row from the grid.">Remove Selected</button>
    <button id="btnNewCsv" title="Create a blank CSV file with the correct column headers to fill in externally.">New CSV</button>
    <button id="btnImportCsv" title="Import printer rows from a CSV file.">Import CSV</button>
    <button id="btnImportPrinters" class="platform-windows-only" title="Import already-configured printers from this computer.">Import Printers</button>
    <button id="btnGetDevmode" class="platform-windows-only" title="Capture the current DEVMODE (print defaults) and Device Settings from every checked row's local printer.">Get DEVMODE</button>
    <span class="spacer"></span>
    <button class="primary" id="btnDeploy" title="Deploy every checked row: create/update ports, drivers, and printer objects, then apply print configuration.">Deploy Checked Printers</button>
    <button class="danger" id="btnStop" disabled title="Stop after the row currently in progress finishes - no further row will start.">STOP</button>
  </div>

  <div class="grid-wrap">
    <table class="grid">
      <thead>
        <tr>
          <th title="${tip('selectAllHeader')}"><input type="checkbox" id="selectAllHeader" title="${tip('selectAllHeader')}"></th>
          <th title="${tip('name')}">Name</th>
          <th title="${tip('ip')}">IP</th>
          <th title="${tip('manufacturer')}">Manufacturer</th>
          <th title="${tip('driver')}">Driver</th>
          <th class="platform-windows-only" title="${tip('snmpGrid')}">SNMP</th>
          <th title="${tip('mono')}">Mono</th>
          <th title="${tip('oneSided')}">1-sided</th>
          <th class="platform-windows-only" title="${tip('useExistingPort')}">UEP</th>
          <th class="platform-windows-only" title="Capture or browse to this row's DEVMODE (print defaults) and Device Settings, applied last during Deploy."></th>
          <th title="Double-click to remove this one row, without needing to check it first."></th>
        </tr>
      </thead>
      <tbody id="gridBody"></tbody>
    </table>
  </div>

  <div class="log-panel">
    <div class="log-header">
      <span>Log</span>
      <span class="spacer"></span>
      <button id="btnClearLog" title="Clear the log output below.">Clear Log</button>
    </div>
    <pre id="logOutput"></pre>
  </div>
`;

const el = (id) => document.getElementById(id);
let lastValidSalesChainId = '';

// Locks the UI immediately, before init()'s own async catalog/settings
// loading even starts - init() used to reach renderGrid() (the only other
// applySalesChainGate()/updateDeployButtonEnabled() call site at startup)
// only after several awaited Wails calls resolved, leaving every control
// fully clickable for however long that took (observed: a few seconds)
// before either ever kicked in. state.salesChainId is always '' and
// state.rows is always [] this early, so both always lock/disable on
// startup regardless - this just needs to happen synchronously, right after
// the elements they operate on exist. updateDeployButtonEnabled() is
// declared further down but hoisted, so calling it here is safe.
applySalesChainGate();
updateDeployButtonEnabled();

// Windows reserved device names (case-insensitive, whole-string only - "PRN1"
// or "COMPANY" are fine, only an exact "PRN"/"COM1"/etc is reserved).
const RESERVED_DEVICE_NAME_RE = /^(CON|PRN|AUX|NUL|COM[0-9]|LPT[0-9])$/i;

// Strips everything except letters, digits, hyphen, and underscore - already
// enough on its own to rule out every DOS/shell-reserved character
// (\/:*?"<>| on Windows, / and : on macOS) without needing to enumerate them.
function sanitizeSalesChainId(raw) {
    return raw.replace(/[^A-Za-z0-9_-]/g, '');
}

// Rejects leading zeros (e.g. "010") along with the obvious out-of-range/
// malformed cases - some systems read a leading-zero octet as octal, which
// makes for a genuinely ambiguous address rather than just a stylistic
// quirk, so treating it as invalid here is deliberate, not an oversight.
function isValidIPv4(s) {
    const m = /^(\d{1,3})\.(\d{1,3})\.(\d{1,3})\.(\d{1,3})$/.exec(s);
    if (!m) return false;
    return m.slice(1).every((part) => {
        if (part.length > 1 && part[0] === '0') return false;
        const n = Number(part);
        return n >= 0 && n <= 255;
    });
}

// A row's IP field is only ever usable for Deploy as a real IPv4 address or
// the literal (case-insensitive) "NUL" - blank, a bare subnet prefix still
// missing its last octet, or any other placeholder text all count as not
// yet ready. Used both for Deploy Checked Printers' own enabled state and
// the IP field's yellow "needs a value" highlight.
function isValidPortValue(ip) {
    const v = (ip || '').trim();
    return v.toUpperCase() === 'NUL' || isValidIPv4(v);
}

// A row's Name is only "set" if it's more than just whitespace - a lone
// space would otherwise pass a bare truthiness/empty-string check while
// still being useless as an actual printer object name.
function isValidName(name) {
    return (name || '').trim().length > 0;
}

// Applies value to both the Save ID field and state, sanitized the
// same way live typing is - used for the field's own input handler and for
// loading a saved configuration, so an old/hand-edited file can't bypass the
// same restriction. A reserved device name can't be fixed by stripping
// characters (it's already all "valid" ones), so it's rejected outright: live
// typing reverts to whatever was there just before the keystroke that would
// have completed it (rejectReservedAsEmpty: false, the default), while
// loading a whole new value from a file has no such "just before" to revert
// to, so it resets to empty instead (rejectReservedAsEmpty: true).
function setSalesChainId(value, {rejectReservedAsEmpty = false} = {}) {
    const v = sanitizeSalesChainId(value || '');
    state.salesChainId = RESERVED_DEVICE_NAME_RE.test(v)
        ? (rejectReservedAsEmpty ? '' : lastValidSalesChainId)
        : v;
    lastValidSalesChainId = state.salesChainId;
    el('salesChainId').value = state.salesChainId;
    el('salesChainId').classList.toggle('input-needs-value', state.salesChainId === '');
    applySalesChainGate();
    updateDeployButtonEnabled();
}

// Save ID is foundational - it's what every saved config, DEVMODE
// .bin, and driver-data sidecar is named after - so nothing else in PDT is
// usable until it has a value, to rule out ever configuring/deploying under
// the wrong job's ID by mistake. Exceptions: the field itself; Open
// Configuration (which can load a value from a saved file); Settings
// (app-wide preferences - Preinstall/Configuration Files Base Path,
// manufacturer URLs/order - that have nothing to do with any particular
// job); Write to Flash Drive, Sync, Spooler, Refresh, and the Drivers-folder
// button (also job-independent - stamping out this laptop's whole
// Drivers/Configs folders, topping up an existing flash drive's own Drivers
// folder, restarting the one Print Spooler service shared by every queue on
// the machine, rescanning the Drivers folder in place, and opening it in
// File Explorer all have nothing to do with one particular Save ID);
// the Defaults panel's Manufacturer dropdown and
// Check for Updates button (also job-independent - picking a manufacturer
// and opening its configured download page touches no SalesChain-ID-named
// file, and this is exactly how the no-drivers banner's own bootstrap
// workflow - pick a Manufacturer, Check for Updates, Refresh - is meant to
// work on a fresh install, before there's any job to name yet) - each along
// with
// everything inside its own modal/dropdown, so it stays fully usable, not
// just openable; the shared confirm dialog (#confirmBackdrop, showConfirm())
// those job-independent flows also pop up through - Write to Flash Drive's
// own "Erase and Format"/Cancel buttons landed disabled with an empty
// Save ID before this was added, confirmed live, since that dialog
// lives outside #flashDriveBackdrop's own already-exempt subtree. The other
// callers of showConfirm() (Reset Configuration, Export Configs) are
// themselves gated by this same sweep, so their own confirm popups are
// simply unreachable while locked either way - exempting the dialog itself
// changes nothing for them; Deploy Checked Printers, whose
// enabled state is entirely owned by updateDeployButtonEnabled() instead
// (IP-validity, not just Save ID, decides that button - see its own
// comment for why that needs to be fully separate from this generic sweep);
// and STOP, which must stay clickable for the entire length of an in-progress
// deploy even if someone edits Save ID mid-run - it's the one button
// that would be actively harmful to lock at the exact moment it's needed.
//
// Unconditional: every non-exempt control is set to exactly `locked` on
// every call, recomputed fresh each time rather than remembered - an
// earlier version tried to track "did the gate itself disable this control"
// via a dataset marker, so a control someone else had independently
// disabled (portPrefixText while its checkbox is unchecked, say) wouldn't
// get blindly re-enabled on unlock. In practice that history-dependent
// bookkeeping could itself end up wrong depending on call order (which is
// exactly the class of bug that produced this function's own git history),
// so the whole approach was replaced with this: lock/unlock is always
// unconditional here, and the one control with its own extra condition
// beyond "Save ID is set" (portPrefixText) gets that condition
// re-asserted right after, every time - see updatePortPrefixTextEnabled().
function applySalesChainGate() {
    const locked = !state.salesChainId;
    document.body.classList.toggle('sales-chain-locked', locked);
    const exemptIds = new Set(['btnOpenConfig', 'salesChainId', 'btnSettings', 'btnFlashDrive', 'btnRefreshDrivers', 'btnSyncFlashDrive', 'btnOpenDriversFolder', 'btnSpooler', 'btnDeploy', 'btnStop', 'defMfg', 'btnCheckUpdates']);
    for (const c of document.querySelectorAll('#app button, #app input, #app select')) {
        if (exemptIds.has(c.id) || c.closest('#settingsBackdrop') || c.closest('#flashDriveBackdrop') || c.closest('#spoolerDropdown') || c.closest('#confirmBackdrop')) continue;
        c.disabled = locked;
    }
    updatePortPrefixTextEnabled();
    updateSnmpCommunityEnabled();
}

// portPrefixText is enabled only when BOTH Save ID is set (the
// generic gate's own condition) AND its own "Port name prefix" checkbox is
// checked - a second, narrower condition the generic sweep above knows
// nothing about, so it always needs reasserting right after that sweep runs.
function updatePortPrefixTextEnabled() {
    const input = el('portPrefixText');
    if (input) input.disabled = !state.salesChainId || !state.portPrefixEnabled;
}

// Same reasoning as updatePortPrefixTextEnabled(), for the Defaults panel's
// own SNMP community string field: enabled only when Save ID is set
// AND the SNMP checkbox next to it is checked.
function updateSnmpCommunityEnabled() {
    const input = el('defSnmpCommunity');
    const checkbox = el('defSnmp');
    if (input && checkbox) input.disabled = !state.salesChainId || !checkbox.checked;
}

let audioCtx = null;

// A short synthesized beep - no audio asset to embed/load, just a couple of
// Web Audio nodes torn down again as soon as the tone finishes. Wrapped in
// try/catch since audio is a nice-to-have here, never something a rejected
// keystroke should be blocked by if it's unavailable (no output device, or a
// browser autoplay policy silently declining it).
function playInvalidDing() {
    try {
        audioCtx = audioCtx || new (window.AudioContext || window.webkitAudioContext)();
        const osc = audioCtx.createOscillator();
        const gain = audioCtx.createGain();
        osc.type = 'square';
        osc.frequency.value = 880;
        gain.gain.setValueAtTime(0.15, audioCtx.currentTime);
        gain.gain.exponentialRampToValueAtTime(0.0001, audioCtx.currentTime + 0.15);
        osc.connect(gain);
        gain.connect(audioCtx.destination);
        osc.start();
        osc.stop(audioCtx.currentTime + 0.15);
    } catch {
        // Audio is a nice-to-have, not a requirement - see comment above.
    }
}

// Flashes input red and dings - called once per rejected keystroke (see the
// Save ID input handler). Removing the class before re-adding it (with
// a reflow forced in between) restarts the CSS animation even if the
// previous flash from a rapid-fire rejected keystroke hasn't finished yet,
// rather than a no-op re-add the browser would otherwise ignore.
function flashInvalidInput(input) {
    input.classList.remove('input-invalid-flash');
    void input.offsetWidth;
    input.classList.add('input-invalid-flash');
    playInvalidDing();
}

// isMac() is the one check every platform-specific call site below uses,
// rather than comparing state.platform directly everywhere - "darwin" is the
// only other value App.Platform ever returns today, but reading this as "is
// the reduced macOS UI active" is clearer at each call site than the literal
// string.
function isMac() {
    return state.platform === 'darwin';
}

async function init() {
    state.platform = await App.Platform();
    document.body.classList.add(state.platform === 'darwin' ? 'platform-darwin' : 'platform-windows');

    state.manufacturers = await App.Manufacturers();
    state.allManufacturers = await App.AllManufacturers();
    el('defMfg').innerHTML = state.manufacturers.map(m => `<option value="${m}">${m}</option>`).join('');
    await resetDefaultsPanel();

    const status = await App.GetCatalogStatus();
    if (!status.ok) {
        const warn = el('catalogWarning');
        warn.hidden = false;
        warn.textContent = `Driver catalog failed to load: ${status.error}`;
    } else if (!status.hasDrivers) {
        el('noDriversBanner').hidden = false;
    }

    state.settings = await App.GetSettings();

    renderGrid();
    wireEvents();
    setupDefaultsComboboxes();
    if (!isMac()) {
        refreshSpoolerButtonState(); // not awaited - shouldn't delay the rest of startup - Windows-only, no CUPS-service-restart analog (see this port's own "explicitly out of scope" notes)
    }

    // Write to Flash Drive can't overwrite the exact exe it's currently
    // running from (Windows refuses outright - confirmed live; macOS allows
    // it technically, but stamping a running copy out onto more drives still
    // makes no sense as a workflow either way) - disabled outright when
    // running portably from removable media itself, rather than failing at
    // click time. Set once, directly, rather than through
    // applySalesChainGate()'s own exemption list - that sweep skips exempt
    // elements entirely (see its own doc comment), so this sticks regardless
    // of Save ID state.
    if (await App.IsRunningFromRemovableDrive()) {
        const btn = el('btnFlashDrive');
        btn.disabled = true;
        btn.title = 'Write to Flash Drive is unavailable when running PDT from a flash drive itself - use an installed copy instead.';
    }

    EventsOn('deploy-progress', (result) => onDeployProgress(result));
    EventsOn('flashcopy-progress', (progress) => updateFlashCopyProgress(progress));

    // Startup overlay: everything above this point runs before wireEvents()
    // attaches a single event listener, so clicking anything during that
    // window previously did nothing with no indication why (confirmed live -
    // BuildCatalog scanning/extracting a real Drivers folder is easily slow
    // enough to notice). Hiding this last, only once the app is actually
    // fully interactive, is what actually fixes that rather than just
    // hiding the symptom.
    el('startupOverlay').hidden = true;
}

function logLevelClass(line) {
    if (line.includes('[ERR]')) return 'log-line-err';
    if (line.includes('[WARN]')) return 'log-line-warn';
    if (line.includes('[OK]')) return 'log-line-ok';
    return 'log-line-info';
}

function appendLog(lines) {
    for (const line of lines) {
        state.logLines.push(line);
    }
    renderLog();
}

// Matches the Go side's own log timestamp format exactly (internal/printer/
// log.go: "2006-01-02 15:04:05") - toLocaleString() used to be used here
// instead, which is locale-dependent (typically M/D/YYYY, H:MM:SS AM/PM in
// en-US) and made every frontend-originated log line visibly inconsistent
// with every line Deploy itself writes.
function formatLogTimestamp(d) {
    const pad = (n) => String(n).padStart(2, '0');
    return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())} ${pad(d.getHours())}:${pad(d.getMinutes())}:${pad(d.getSeconds())}`;
}

// Every one-off status message (an operation's result, a validation
// message) goes through here rather than a separate status bar - a status
// bar wide enough for a long message pushed Deploy Checked Printers onto its
// own line (see the toolbar's own history), and a timestamped log line is
// strictly more useful anyway since it doesn't get overwritten by whatever
// happens next. level is one of 'OK'/'WARN'/'ERR'/'INFO', matching
// logLevelClass's own recognized markers.
function logStatus(level, text) {
    appendLog([`${formatLogTimestamp(new Date())} [${level}] ${text}`]);
}

function renderLog() {
    const out = el('logOutput');
    out.innerHTML = state.logLines
        .map(l => `<span class="${logLevelClass(l)}">${escapeHtml(l)}</span>`)
        .join('\n');
    out.scrollTop = out.scrollHeight;
}

function escapeHtml(s) {
    return s.replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;');
}

// --- Combobox (Driver) ---
//
// Replaces an earlier <input list=...><datalist> attempt: native datalist
// only shows suggestions once the user starts typing (no "click to see
// everything available" like the original WinForms ComboBox this is
// replacing), and its filtering behavior is inconsistent across browsers -
// it didn't actually deliver "type to filter" in a way that felt like a real
// combobox. This is a small, self-contained dropdown instead: every instance
// owns its own DOM elements and closure state (items/highlighted), so - unlike
// the WinForms DataGridView bug that forced a full rewrite of the original
// tool's grid, rooted in cells sharing a single live editing control - there
// is no shared state for one row's combobox to leak into another's.
//
// input/list are the two DOM elements (an <input> and an adjacent container
// for the dropdown items). fetchCandidates(currentText) resolves to the list
// of strings to offer, given whatever the input is showing right now, so it
// naturally reflects the row/defaults' latest Manufacturer selection with no
// separate invalidation step needed. onChange(value) is called with every
// keystroke and on commit.
// onEnter (optional), if given, fires on a plain Enter that isn't committing
// a highlighted dropdown item - the "add a new row" shortcut piggybacks on
// this rather than stealing Enter outright, so committing a suggestion still
// always takes priority when the dropdown is actually open.
function setupCombobox(input, list, fetchCandidates, onChange, onEnter) {
    let items = [];
    let highlighted = -1;
    let closeTimer = null;

    function render() {
        list.innerHTML = items
            .map((it, i) => `<div class="combo-item${i === highlighted ? ' active' : ''}" data-i="${i}">${escapeHtml(it)}</div>`)
            .join('');
        list.hidden = items.length === 0;
    }

    async function refresh() {
        const text = input.value;
        const results = await fetchCandidates(text);
        if (input.value !== text) return; // stale response for text the user has since changed
        items = results;
        highlighted = -1;
        render();
    }

    function commit(value) {
        clearTimeout(closeTimer);
        input.value = value;
        onChange(value);
        list.hidden = true;
    }

    input.addEventListener('focus', refresh);
    input.addEventListener('input', () => {
        onChange(input.value);
        refresh();
    });
    input.addEventListener('keydown', (e) => {
        const dropdownOpen = !list.hidden && items.length > 0;
        if (e.key === 'ArrowDown' && dropdownOpen) {
            e.preventDefault();
            highlighted = Math.min(highlighted + 1, items.length - 1);
            render();
        } else if (e.key === 'ArrowUp' && dropdownOpen) {
            e.preventDefault();
            highlighted = Math.max(highlighted - 1, 0);
            render();
        } else if (e.key === 'Enter') {
            if (dropdownOpen && highlighted >= 0) {
                e.preventDefault();
                commit(items[highlighted]);
            } else if (onEnter) {
                onEnter();
            }
        } else if (e.key === 'Escape' && dropdownOpen) {
            list.hidden = true;
        }
    });
    list.addEventListener('mousedown', (e) => {
        const itemEl = e.target.closest('.combo-item');
        if (!itemEl) return;
        e.preventDefault(); // keep focus in input rather than losing it to the click
        commit(items[parseInt(itemEl.dataset.i, 10)]);
    });
    input.addEventListener('blur', () => {
        // A click on the list fires its own mousedown (and commits) before
        // this runs; a short delay lets that happen first. A click truly
        // outside both elements still needs this to actually hide the list.
        closeTimer = setTimeout(() => { list.hidden = true; }, 150);
    });
}

// Deploy progress arrives one event per row, in the same order the rows
// were submitted in (see deploy() below) - correlated by position via
// activeDeploy rather than by row.Name, which the tool has never required to
// be unique. Only the affected row's <tr> is touched (never a full
// renderGrid()), since a multi-row deploy can run for minutes and the user
// may well be editing a different row's fields in the meantime - replacing
// the whole tbody would destroy that in-progress input's focus and value.
function onDeployProgress(result) {
    appendLog(result.log);
    if (!state.activeDeploy) return;
    const {rows, nextIndex} = state.activeDeploy;
    const row = rows[nextIndex];
    state.activeDeploy.nextIndex++;
    if (!row) return;

    row._failed = !!result.error;
    row._succeeded = !result.error;
    const tr = document.querySelector(`tr[data-id="${row._id}"]`);
    if (tr) {
        tr.classList.remove('row-failed', 'row-succeeded');
        tr.classList.add(row._failed ? 'row-failed' : 'row-succeeded');
    }
}

// --- Grid rendering ---

function renderGrid() {
    const tbody = el('gridBody');
    tbody.innerHTML = state.rows.map(rowHtml).join('');
    updateSelectAllHeaderState();
    wireRowEvents();
    // Freshly-created row DOM starts fully enabled - re-apply the SalesChain
    // ID gate so it's covered too (relevant if rows are already on the grid
    // from before the ID was cleared back out).
    applySalesChainGate();
    updateDeployButtonEnabled();
}

// Deploy Checked Printers is only ever meaningfully clickable once every
// checked row has a printer port Deploy can actually act on - a real IPv4
// address or the literal "NUL" (see isValidPortValue). Fully self-contained
// (also checks state.deploying and state.salesChainId itself) rather than
// composing with applySalesChainGate()'s generic dataset.gateLocked sweep,
// which has no concept of IP validity and would otherwise just blindly
// re-enable this button the moment Save ID gets a value regardless of
// whether the rows are actually ready - see the gate's own exemption list,
// which leaves btnDeploy out for exactly this reason.
function updateDeployButtonEnabled() {
    const btn = el('btnDeploy');
    if (!btn) return;
    if (state.deploying || !state.salesChainId) {
        btn.disabled = true;
        return;
    }
    const checked = state.rows.filter(r => r.select);
    btn.disabled = !(checked.length > 0 && checked.every(r => isValidPortValue(r.ip)));
}

function rowHtml(r) {
    const cls = r._failed ? 'row-failed' : (r._succeeded ? 'row-succeeded' : '');
    return `
    <tr data-id="${r._id}" class="${cls}">
      <td class="checkbox-cell"><input type="checkbox" class="row-select" ${r.select ? 'checked' : ''} title="${tip('select')}"></td>
      <td><input type="text" class="row-name${isValidName(r.name) ? '' : ' input-needs-value'}" value="${attr(r.name)}" title="${tip('name')}"></td>
      <td><input type="text" class="row-ip${isValidPortValue(r.ip) ? '' : ' input-needs-value'}" value="${attr(r.ip)}" placeholder="or NUL" title="${tip('ip')}"></td>
      <td>${mfgSelectHtml(r)}</td>
      <td><div class="combo"><input type="text" class="row-driver${r.driver ? '' : ' input-needs-value'}" value="${attr(r.driver)}" title="${tip('driver')}"><div class="combo-list" hidden></div></div></td>
      <td class="platform-windows-only"><input type="text" class="row-snmp" value="${attr(r.snmpCommunity)}" placeholder="off" title="${tip('snmpGrid')}"></td>
      <td class="checkbox-cell"><input type="checkbox" class="row-mono" ${r.mono ? 'checked' : ''} title="${tip('mono')}"></td>
      <td class="checkbox-cell"><input type="checkbox" class="row-onesided" ${r.oneSided ? 'checked' : ''} title="${tip('oneSided')}"></td>
      <td class="checkbox-cell platform-windows-only"><input type="checkbox" class="row-uep" ${r.useExistingPort ? 'checked' : ''} title="${tip('useExistingPort')}"></td>
      <td class="platform-windows-only">${devModeButtonHtml(r)}</td>
      <td class="checkbox-cell"><button class="row-remove" title="Double-click to remove this row">&times;</button></td>
    </tr>`;
}

function devModeButtonHtml(r) {
    const set = !!r.devModeFile;
    const label = set ? 'DEVMODE SET' : 'Get DEVMODE';
    const cls = set ? 'row-devmode set' : 'row-devmode';
    const title = set
        ? `Captured from ${attr(r.devModeFile)}. Click to replace it.`
        : 'Capture this row\'s DEVMODE and Device Settings from a local printer of the same name, or browse to an existing .bin file.';
    return `<button type="button" class="${cls}" title="${title}">${label}</button>`;
}

function mfgSelectHtml(r) {
    const opts = state.manufacturers.map(m => `<option value="${m}" ${m === r.manufacturer ? 'selected' : ''}>${m}</option>`).join('');
    const cls = r.manufacturer ? 'row-mfg' : 'row-mfg input-needs-value';
    return `<select class="${cls}" title="${tip('manufacturer')}">${opts}</select>`;
}

// refreshManufacturerDropdowns: re-fetches state.manufacturers (reflecting
// any just-saved Settings > General reorder) and rebuilds every already-
// rendered Manufacturer <select>'s <option> list in place - the Defaults
// panel's and every existing grid row's - each preserving its own current
// selection rather than resetting to the new first option.
async function refreshManufacturerDropdowns() {
    state.manufacturers = await App.Manufacturers();
    const optsHtml = (selected) => state.manufacturers
        .map(m => `<option value="${m}" ${m === selected ? 'selected' : ''}>${m}</option>`).join('');

    const defMfg = el('defMfg');
    defMfg.innerHTML = optsHtml(defMfg.value);

    for (const select of document.querySelectorAll('table.grid select.row-mfg')) {
        select.innerHTML = optsHtml(select.value);
    }
}

function attr(s) {
    return (s || '').replace(/&/g, '&amp;').replace(/"/g, '&quot;');
}

function wireRowEvents() {
    const tbody = el('gridBody');
    for (const tr of tbody.querySelectorAll('tr')) {
        const id = parseInt(tr.dataset.id, 10);
        const row = state.rows.find(r => r._id === id);
        if (!row) continue;

        tr.querySelector('.row-select').addEventListener('change', (e) => {
            row.select = e.target.checked;
            updateSelectAllHeaderState();
            updateDeployButtonEnabled();
        });
        const nameInput = tr.querySelector('.row-name');
        nameInput.addEventListener('input', (e) => {
            row.name = e.target.value;
            nameInput.classList.toggle('input-needs-value', !isValidName(row.name));
        });
        nameInput.addEventListener('keydown', (e) => { if (e.key === 'Enter') addPrinterRow(true); });

        const ipInput = tr.querySelector('.row-ip');
        ipInput.addEventListener('input', (e) => {
            row.ip = e.target.value;
            ipInput.classList.toggle('input-needs-value', !isValidPortValue(row.ip));
            updateDeployButtonEnabled();
        });
        ipInput.addEventListener('keydown', (e) => { if (e.key === 'Enter') addPrinterRow(true); });
        // The only text field where tabbing in should land the cursor at the
        // end of whatever's already there (e.g. a subnet prefix like
        // "10.1.1.") rather than select-all - so typing immediately appends
        // just the last octet instead of overwriting the whole thing. Every
        // other field keeps the browser's normal tab-in select-all behavior.
        // Deferred a tick: the browser applies its own select-all after
        // 'focus' fires, so setting the caret position inside the handler
        // itself would just get overridden a moment later.
        ipInput.addEventListener('focus', (e) => {
            const target = e.target;
            setTimeout(() => {
                const end = target.value.length;
                target.setSelectionRange(end, end);
            }, 0);
        });
        tr.querySelector('.row-mono').addEventListener('change', (e) => { row.mono = e.target.checked; });
        tr.querySelector('.row-onesided').addEventListener('change', (e) => { row.oneSided = e.target.checked; });

        if (!isMac()) {
            tr.querySelector('.row-snmp').addEventListener('input', (e) => { row.snmpCommunity = e.target.value; });
            tr.querySelector('.row-uep').addEventListener('change', (e) => { row.useExistingPort = e.target.checked; });
            wireDevModeButton(tr, row);
        }

        const driverCombo = tr.querySelector('.row-driver').closest('.combo');
        const driverInput = driverCombo.querySelector('input');
        setupCombobox(
            driverInput,
            driverCombo.querySelector('.combo-list'),
            // Model has no field of its own - typing part of a model name
            // (e.g. "MA4500") straight into Driver's own filter text narrows
            // the list the same way a separate Model field would, since a
            // driver/PPD name that's model-specific already spells the model
            // out (Kyocera's, mainly, on Windows; a macOS PPD's own filename
            // usually does too - see driver.ppdMatchLabel).
            (filterText) => App.DriverCandidates(row.manufacturer, '', filterText),
            (value) => {
                row.driver = value;
                driverInput.classList.toggle('input-needs-value', !value);
            },
            () => addPrinterRow(true),
        );

        const mfgSelect = tr.querySelector('.row-mfg');
        mfgSelect.addEventListener('change', (e) => {
            row.manufacturer = e.target.value;
            row.driver = '';
            renderGrid();
        });

        // Double-click rather than a confirm() dialog - fast for someone who
        // means it, but a stray single click can't nuke a row by accident.
        tr.querySelector('.row-remove').addEventListener('dblclick', () => {
            state.rows = state.rows.filter(r => r._id !== row._id);
            renderGrid();
        });
    }
}

function wireDevModeButton(tr, row) {
    tr.querySelector('.row-devmode').addEventListener('click', () => captureOrBrowseDevMode(row));
}

// Replaces just one row's DEVMODE <td> in place (never a full renderGrid())
// and re-wires its button - same reasoning onDeployProgress documents for
// every other in-place row patch: a long-running capture on one row
// shouldn't disturb focus/in-progress edits elsewhere in the grid.
function patchRowDevModeButton(row) {
    const tr = document.querySelector(`tr[data-id="${row._id}"]`);
    if (!tr) return;
    const td = tr.querySelector('.row-devmode').closest('td');
    td.innerHTML = devModeButtonHtml(row);
    wireDevModeButton(tr, row);
}

// Per-row DEVMODE button click handler: try a live capture from a local
// printer of the same name first (the reference-machine case this feature
// exists for); if none is found, fall back to browsing for an existing
// .bin file instead (e.g. one captured elsewhere). Re-clicking an
// already-"DEVMODE SET" row replaces it the same way.
async function captureOrBrowseDevMode(row) {
    if (!state.salesChainId) {
        logStatus('WARN', 'Set a Save ID before capturing a DEVMODE.');
        return;
    }
    const tr = document.querySelector(`tr[data-id="${row._id}"]`);
    const btn = tr?.querySelector('.row-devmode');
    if (btn) btn.disabled = true;
    try {
        let result = await App.CaptureDevModeForPrinter(state.salesChainId, row.name);
        if (result.error) {
            result = await App.BrowseDevModeFile(state.salesChainId, row.name);
        }
        if (result.canceled) {
            logStatus('INFO', 'DEVMODE capture canceled.');
            return;
        }
        if (result.error) {
            logStatus('ERR', `Could not capture DEVMODE for "${row.name}": ${result.error}`);
            return;
        }
        row.devModeFile = result.fileName;
        patchRowDevModeButton(row);
        logStatus('OK', `Captured DEVMODE for "${row.name}".`);
    } finally {
        if (btn) btn.disabled = false;
    }
}

function updateSelectAllHeaderState() {
    const header = el('selectAllHeader');
    header.checked = state.rows.length > 0 && state.rows.every(r => r.select);
}

function setupDefaultsComboboxes() {
    setupCombobox(
        el('defDriver'),
        el('defDriverList'),
        // No separate Model field here either - see the grid row driver
        // combobox's own comment.
        (filterText) => App.DriverCandidates(el('defMfg').value, '', filterText),
        (value) => { el('defDriver').classList.toggle('input-needs-value', !value); },
    );
}

// --- Toolbar actions ---

// Resets the Defaults panel to its fresh-launch values - shared by init()
// (the initial paint) and resetConfiguration() (Reset Configuration), so
// there's exactly one place that knows what "default" means instead of two
// copies drifting apart.
async function resetDefaultsPanel() {
    const mfgSelect = el('defMfg');
    mfgSelect.value = state.manufacturers[0] || '';
    mfgSelect.classList.toggle('input-needs-value', !mfgSelect.value);
    el('defDriver').value = mfgSelect.value ? await App.DefaultDriverFor(mfgSelect.value) : '';
    el('defDriver').classList.toggle('input-needs-value', !el('defDriver').value);
    if (!isMac()) {
        el('portPrefixEnabled').checked = false;
        state.portPrefixEnabled = false;
        el('portPrefixText').value = '';
        state.portPrefixText = '';
        updatePortPrefixTextEnabled();
        el('defUseExistingPort').checked = false;
        el('defSnmp').checked = false;
        el('defSnmpCommunity').value = 'public';
        updateSnmpCommunityEnabled();
        el('defApf').checked = false;
    }
    el('defSubnet').value = '';
    el('defMono').checked = true;
    el('defOneSided').checked = true;
}

// Reset Configuration: takes the whole app - grid, Save ID, and the
// Defaults panel - back to how it looks right after launching PDT. Confirms
// first whenever there's actually something to lose (an empty, freshly-
// launched app has nothing worth confirming).
async function resetConfiguration() {
    if (state.rows.length > 0 || state.salesChainId) {
        const ok = await showConfirm({
            title: 'Warning',
            message: 'Reset PDT to its default settings? This clears every row, the Save ID, and the Defaults panel.',
            okLabel: 'Reset',
        });
        if (!ok) return;
    }
    state.rows = [];
    setSalesChainId('');
    await resetDefaultsPanel();
    renderGrid();
    await App.ResetConfigPath();
    logStatus('OK', 'Reset PDT to default settings.');
}

// Adds a row using the Defaults panel's current values (the "Add Printer"
// button's own logic, also reused by the Enter-key shortcut below) and, when
// focusNewRow is true, moves focus straight to the new row's Name field -
// so pressing Enter repeatedly from inside the grid reads naturally as
// "commit this row, start the next one" rather than leaving focus behind on
// whatever row you were just on.
function addPrinterRow(focusNewRow = false) {
    const subnet = el('defSubnet').value.trim();
    const ip = subnet ? (subnet.endsWith('.') ? subnet : subnet + '.') : '';
    const row = newRow({
        ip,
        manufacturer: el('defMfg').value,
        driver: el('defDriver').value,
        snmpCommunity: !isMac() && el('defSnmp').checked ? el('defSnmpCommunity').value : '',
        mono: el('defMono').checked,
        oneSided: el('defOneSided').checked,
        useExistingPort: !isMac() && el('defUseExistingPort').checked,
        advancedPrintingFeatures: !isMac() && el('defApf').checked,
    });
    state.rows.push(row);
    renderGrid();
    if (focusNewRow) {
        const tr = document.querySelector(`tr[data-id="${row._id}"]`);
        tr?.querySelector('.row-name')?.focus();
    }
}

function wireEvents() {
    el('salesChainId').addEventListener('input', (e) => {
        const raw = e.target.value;
        const caretWasAtEnd = e.target.selectionStart === raw.length;
        setSalesChainId(raw);
        // raw differs from what actually got applied whenever this keystroke
        // was rejected outright - either a disallowed character got stripped,
        // or the value would have completed a reserved device name and was
        // reverted - never for an ordinary edit, since sanitizing an
        // already-valid string is always a no-op.
        if (raw !== state.salesChainId) {
            flashInvalidInput(el('salesChainId'));
        }
        // setSalesChainId() re-set .value above, which drops focus/caret
        // state for nothing - restore it so typing feels normal.
        el('salesChainId').focus();
        if (caretWasAtEnd) {
            const end = el('salesChainId').value.length;
            el('salesChainId').setSelectionRange(end, end);
        }
    });

    if (!isMac()) {
        el('portPrefixEnabled').addEventListener('change', (e) => {
            state.portPrefixEnabled = e.target.checked;
            updatePortPrefixTextEnabled();
        });
        el('portPrefixText').addEventListener('input', (e) => { state.portPrefixText = e.target.value; });

        el('defSnmp').addEventListener('change', updateSnmpCommunityEnabled);
    }

    el('defMfg').addEventListener('change', async (e) => {
        e.target.classList.toggle('input-needs-value', !e.target.value);
        el('defDriver').value = await App.DefaultDriverFor(el('defMfg').value);
        el('defDriver').classList.toggle('input-needs-value', !el('defDriver').value);
    });

    el('selectAllHeader').addEventListener('change', (e) => {
        for (const r of state.rows) r.select = e.target.checked;
        renderGrid();
    });

    el('btnAddRow').addEventListener('click', () => addPrinterRow());

    el('btnRemoveRow').addEventListener('click', () => {
        state.rows = state.rows.filter(r => !r.select);
        renderGrid();
    });

    el('btnNewCsv').addEventListener('click', async () => {
        const result = await App.NewCsvTemplate();
        if (!result.canceled) logStatus('OK', `Wrote new CSV template to ${result.path}`);
    });

    el('btnImportCsv').addEventListener('click', async () => {
        const result = await App.ImportCsv();
        if (result.canceled) return;
        state.rows.push(...result.rows.map(pr => printerRowToRow(pr, true)));
        renderGrid();
        logStatus('OK', `Imported ${result.rows.length} row(s) from CSV.`);
    });

    el('btnImportPrinters').addEventListener('click', openImportPrintersModal);
    el('btnImportPrintersCancel').addEventListener('click', closeImportPrintersModal);
    el('btnImportPrintersConfirm').addEventListener('click', confirmImportPrinters);
    wireBackdropDismiss('importPrintersBackdrop', closeImportPrintersModal);

    el('btnGetDevmode').addEventListener('click', async () => {
        const selected = state.rows.filter(r => r.select);
        if (selected.length === 0) {
            logStatus('WARN', 'No rows checked.');
            return;
        }
        if (!state.salesChainId) {
            logStatus('WARN', 'Set a Save ID before capturing DEVMODE configs.');
            return;
        }
        let captured = 0;
        for (const row of selected) {
            const result = await App.CaptureDevModeForPrinter(state.salesChainId, row.name);
            if (!result.error) {
                row.devModeFile = result.fileName;
                patchRowDevModeButton(row);
                captured++;
            }
        }
        logStatus('OK', `Captured DEVMODE for ${captured} of ${selected.length} checked row(s).`);
    });

    el('btnOpenConfig').addEventListener('click', async () => {
        const result = await App.OpenConfiguration();
        if (result.canceled) return;
        setSalesChainId(result.config.SalesChainId, {rejectReservedAsEmpty: true});
        state.rows = (result.config.Printers || []).map(savedRowToRow);
        renderGrid();
        logStatus('OK', `Loaded configuration (${state.rows.length} row(s)).`);
    });

    el('btnSaveConfig').addEventListener('click', async () => {
        const cfg = {SalesChainId: state.salesChainId, Printers: state.rows.map(rowToSavedRow)};
        const result = await App.SaveConfiguration(cfg);
        if (!result.canceled) logStatus('OK', `Saved configuration to ${result.path}`);
    });

    el('btnResetConfig').addEventListener('click', resetConfiguration);
    el('btnExportConfigs').addEventListener('click', exportConfigs);

    el('btnSpooler').addEventListener('click', (e) => {
        e.stopPropagation();
        el('spoolerMenu').hidden = !el('spoolerMenu').hidden;
    });
    for (const item of document.querySelectorAll('#spoolerMenu .dropdown-item')) {
        item.addEventListener('click', () => {
            el('spoolerMenu').hidden = true;
            controlSpooler(item.dataset.spoolerAction);
        });
    }
    // Closes the Spooler dropdown on any click outside it - the stopPropagation()
    // above on btnSpooler's own click keeps opening it from immediately
    // closing itself via this same listener.
    document.addEventListener('click', (e) => {
        if (!el('spoolerMenu').hidden && !e.target.closest('#spoolerDropdown')) {
            el('spoolerMenu').hidden = true;
        }
    });

    el('btnFlashDrive').addEventListener('click', () => openFlashDriveModal('write'));
    el('btnSyncFlashDrive').addEventListener('click', () => openFlashDriveModal('sync'));
    el('btnFlashDriveCancel').addEventListener('click', closeFlashDriveModal);
    el('btnFlashDriveConfirm').addEventListener('click', confirmWriteToFlashDrive);
    wireBackdropDismiss('flashDriveBackdrop', closeFlashDriveModal);

    wireBackdropDismiss('confirmBackdrop', () => { if (pendingConfirmCancel) pendingConfirmCancel(); });
    wireBackdropDismiss('stopBackdrop', () => { if (pendingStopCancel) pendingStopCancel(); });

    el('btnClearLog').addEventListener('click', () => {
        state.logLines = [];
        renderLog();
    });

    el('btnDeploy').addEventListener('click', deploy);

    el('btnStop').addEventListener('click', async () => {
        const choice = await showStopDialog();
        if (choice === 'cancel') return;
        if (choice === 'force') {
            logStatus('ERR', 'Force Stop requested - closing PDT immediately.');
            App.ForceQuit();
            return;
        }
        el('btnStop').disabled = true;
        await App.StopDeploy();
        logStatus('WARN', 'Stop requested - the current row will finish, then no further row will start.');
    });

    wireSettingsModal();
}

// --- Settings modal ---

// Each manufacturer is a plain always-visible text field - state.allManufacturers
// (every manufacturer PDT knows about) rather than state.manufacturers (only
// those with drivers actually present locally), so a URL can be configured
// here before its drivers are ever added to the local Drivers folder.
function renderSettingsSitesPanel() {
    const panel = el('settingsSitesPanel');
    panel.innerHTML = state.allManufacturers.map(mfg => `
        <label class="modal-field">
          ${mfg}
          <input type="text" class="settings-url" data-mfg="${attr(mfg)}">
        </label>
    `).join('');
}

// renderManufacturerOrderList + wireMfgOrderDragDrop: a plain HTML5
// drag-and-drop reorderable list (no library) for Settings > General's
// Manufacturer sort order - controls the Manufacturer dropdown's order in
// both the Defaults panel and the grid (both read from state.manufacturers,
// itself ordered by App.Manufacturers() per this same saved order).
// Reordering happens live during dragover (moving the dragged <li> directly
// via insertBefore) rather than waiting for a drop event - a common
// lightweight pattern that needs no separate drop handler.
function renderManufacturerOrderList(order) {
    const list = el('mfgOrderList');
    list.innerHTML = (order || state.settings.manufacturerOrder).map(mfg => `
        <li class="mfg-order-item" draggable="true" data-mfg="${attr(mfg)}">
          <span class="mfg-order-handle">&#9776;</span> ${mfg}
        </li>
    `).join('');
    wireMfgOrderDragDrop();
}

function wireMfgOrderDragDrop() {
    const list = el('mfgOrderList');
    let draggedEl = null;

    for (const item of list.querySelectorAll('.mfg-order-item')) {
        item.addEventListener('dragstart', () => {
            draggedEl = item;
            item.classList.add('dragging');
        });
        item.addEventListener('dragend', () => {
            item.classList.remove('dragging');
            draggedEl = null;
        });
        item.addEventListener('dragover', (e) => {
            e.preventDefault();
            if (!draggedEl || draggedEl === item) return;
            const rect = item.getBoundingClientRect();
            const before = (e.clientY - rect.top) < rect.height / 2;
            list.insertBefore(draggedEl, before ? item : item.nextSibling);
        });
    }
}

function currentManufacturerOrder() {
    return Array.from(el('mfgOrderList').querySelectorAll('.mfg-order-item')).map(li => li.dataset.mfg);
}

// --- Import Printers modal ---

// Candidates from the most recent App.EnumerateLocalPrinters() call, indexed the
// same as the checkboxes rendered from them - held here (not in state) since
// it's only ever needed while the modal itself is open.
let importPrintersCandidates = [];

async function openImportPrintersModal() {
    importPrintersCandidates = await App.EnumerateLocalPrinters() || [];
    renderImportPrintersList();
    el('importPrintersGetDevmode').checked = true;
    el('importPrintersBackdrop').hidden = false;
}

function closeImportPrintersModal() {
    el('importPrintersBackdrop').hidden = true;
}

function renderImportPrintersList() {
    const list = el('importPrintersList');
    if (importPrintersCandidates.length === 0) {
        list.innerHTML = '<p class="modal-hint">No local printers found.</p>';
        return;
    }
    list.innerHTML = importPrintersCandidates.map((c, i) => `
        <label class="import-printer-item">
          <input type="checkbox" class="import-printer-check" data-index="${i}" ${c.physical ? 'checked' : ''}>
          <span class="import-printer-name">${attr(c.name)}</span>
          <span class="import-printer-detail">${attr(c.manufacturer || '?')} &middot; ${attr(c.driver)}${c.ip ? ' &middot; ' + attr(c.ip) : ''}</span>
        </label>
    `).join('');
}

// Adds a grid row for each checked candidate (Name/IP/Manufacturer/Driver
// carried over from the local printer object itself), then - if "Get
// DEVMODE" is also checked - immediately captures each new row's DEVMODE,
// the same call the per-row/bulk buttons use, so an already-configured
// reference printer's print defaults are captured in the same step it's
// imported rather than needing a second pass.
async function confirmImportPrinters() {
    const checks = Array.from(el('importPrintersList').querySelectorAll('.import-printer-check'));
    const chosen = checks.filter(c => c.checked).map(c => importPrintersCandidates[c.dataset.index]);
    const getDevmode = el('importPrintersGetDevmode').checked;
    closeImportPrintersModal();
    if (chosen.length === 0) return;

    const newRows = chosen.map(c => newRow({
        name: c.name,
        ip: c.ip || '',
        manufacturer: c.manufacturer || '',
        driver: c.driver || '',
    }));
    state.rows.push(...newRows);
    renderGrid();

    if (!getDevmode) {
        logStatus('OK', `Imported ${newRows.length} printer(s).`);
        return;
    }
    if (!state.salesChainId) {
        logStatus('WARN', `Imported ${newRows.length} printer(s). Set a Save ID to capture DEVMODE configs.`);
        return;
    }
    let captured = 0;
    for (const row of newRows) {
        const result = await App.CaptureDevModeForPrinter(state.salesChainId, row.name);
        if (!result.error) {
            row.devModeFile = result.fileName;
            patchRowDevModeButton(row);
            captured++;
        }
    }
    logStatus('OK', `Imported ${newRows.length} printer(s), captured DEVMODE for ${captured}.`);
}

// --- Export Configs ---

function baseName(path) {
    const parts = path.split(/[\\/]/).filter(Boolean);
    return parts[parts.length - 1] || path;
}

// Shows the "more than one Preinstall subfolder matches" picker and resolves
// to the chosen full path, or null if canceled.
function pickExportFolder(folders) {
    return new Promise((resolve) => {
        el('exportFolderList').innerHTML = folders.map((f, i) => `
            <label class="import-printer-item">
              <input type="radio" name="exportFolderChoice" value="${i}" ${i === 0 ? 'checked' : ''}>
              <span class="import-printer-name" title="${attr(f)}">${attr(baseName(f))}</span>
            </label>
        `).join('');
        el('exportFolderBackdrop').hidden = false;

        const confirmBtn = el('btnExportFolderConfirm');
        const cancelBtn = el('btnExportFolderCancel');
        function cleanup(result) {
            el('exportFolderBackdrop').hidden = true;
            confirmBtn.removeEventListener('click', onConfirm);
            cancelBtn.removeEventListener('click', onCancel);
            resolve(result);
        }
        function onConfirm() {
            const checked = el('exportFolderList').querySelector('input[name="exportFolderChoice"]:checked');
            cleanup(checked ? folders[Number(checked.value)] : null);
        }
        function onCancel() { cleanup(null); }
        confirmBtn.addEventListener('click', onConfirm);
        cancelBtn.addEventListener('click', onCancel);
    });
}

// Shows the "these files already exist at the destination" prompt and
// resolves to 'into' (overwrite), 'new' (fresh timestamped subfolder), or
// null if canceled.
function resolveExportCollision(files) {
    return new Promise((resolve) => {
        el('exportCollisionHint').textContent =
            `${files.length} file(s) already exist in the destination PDT folder. Overwrite them, or save this export to a new, timestamped subfolder instead?`;
        el('exportCollisionList').innerHTML = files.map(f => `<div class="import-printer-item">${escapeHtml(f)}</div>`).join('');
        el('exportCollisionBackdrop').hidden = false;

        const overwriteBtn = el('btnExportCollisionOverwrite');
        const newBtn = el('btnExportCollisionNew');
        const cancelBtn = el('btnExportCollisionCancel');
        function cleanup(result) {
            el('exportCollisionBackdrop').hidden = true;
            overwriteBtn.removeEventListener('click', onOverwrite);
            newBtn.removeEventListener('click', onNew);
            cancelBtn.removeEventListener('click', onCancel);
            resolve(result);
        }
        function onOverwrite() { cleanup('into'); }
        function onNew() { cleanup('new'); }
        function onCancel() { cleanup(null); }
        overwriteBtn.addEventListener('click', onOverwrite);
        newBtn.addEventListener('click', onNew);
        cancelBtn.addEventListener('click', onCancel);
    });
}

// Export Configs: copies every Configs/<SaveID>* file (the saved JSON
// config, captured DEVMODE .bin's, driver-data sidecars) from this flash
// drive to the matching "<SaveID> - <Client> - <Address>" subfolder
// under Preinstall Base Path (Settings > General), so a site-survey folder
// ends up with everything PDT captured on the reference machine before the
// tech ever gets to the actual install. Preinstall Base Path is a folder on
// THIS computer, so the warning up front matters - this only does something
// useful when run on the technician's own laptop, not whatever computer the
// flash drive's Configs folder was captured on.
async function exportConfigs() {
    const proceed = await showConfirm({
        title: 'Confirm Export Location',
        message: 'Export Configs copies this Save ID\'s Configs files to THIS computer\'s Preinstall folder.\n\n' +
            'Continue only if PDT is running on the technician\'s laptop - not the computer the flash drive\'s Configs were captured on.',
        okLabel: 'Continue',
    });
    if (!proceed) return;

    const listResult = await App.ListPreinstallFolders(state.salesChainId);
    if (listResult.error) {
        logStatus('ERR', listResult.error);
        return;
    }
    if (listResult.folders.length === 0) {
        logStatus('ERR', `No Preinstall subfolder found for Save ID "${state.salesChainId}". Create "${state.salesChainId} - <Client> - <Address>" under the configured Preinstall Base Path first.`);
        return;
    }

    let destFolder = listResult.folders[0];
    if (listResult.folders.length > 1) {
        destFolder = await pickExportFolder(listResult.folders);
        if (!destFolder) return;
    }

    const collisionResult = await App.CheckExportCollisions(state.salesChainId, destFolder);
    if (collisionResult.error) {
        logStatus('ERR', collisionResult.error);
        return;
    }
    if (collisionResult.sourceFiles.length === 0) {
        logStatus('WARN', `No Configs files found for Save ID "${state.salesChainId}".`);
        return;
    }

    let mode = 'into';
    if (collisionResult.colliding.length > 0) {
        mode = await resolveExportCollision(collisionResult.colliding);
        if (!mode) return;
    }

    const result = await App.ExportConfigs(state.salesChainId, destFolder, mode);
    if (result.error) {
        logStatus('ERR', `Export Configs failed: ${result.error}`);
        return;
    }
    logStatus('OK', `Exported ${result.copied.length} file(s) to ${result.destPath}. Configs files can now be deleted from this flash drive.`);
}

// --- Spooler ---

// Colors btnSpooler to match the service's actual state - green running,
// red stopped, yellow for anything still settling (a pending transition, or
// a query that failed and left the state simply unknown - treated the same
// as "don't claim it's definitely up or definitely down").
function applySpoolerButtonState(state) {
    const btn = el('btnSpooler');
    btn.classList.remove('spooler-running', 'spooler-stopped', 'spooler-pending');
    if (state === 'running') btn.classList.add('spooler-running');
    else if (state === 'stopped') btn.classList.add('spooler-stopped');
    else btn.classList.add('spooler-pending');
}

async function refreshSpoolerButtonState() {
    const result = await App.SpoolerStatus();
    applySpoolerButtonState(result.error ? 'pending' : result.state);
}

// controlSpooler: action is 'restart'/'start'/'stop', matching each dropdown
// item's data-spooler-action and the Go method name directly. Job-independent
// (this affects every print queue on the machine, not just PDT's own rows),
// so it needs no Save ID and isn't gated by it - same reasoning as
// Settings/Write to Flash Drive. Shows pending (yellow) for the duration of
// the call itself - Restart in particular takes a real, visible moment -
// then the actual resulting state the Go side already re-queried once it
// resolves.
async function controlSpooler(action) {
    const fns = {restart: RestartSpooler, start: StartSpooler, stop: StopSpooler};
    const pastTense = {restart: 'restarted', start: 'started', stop: 'stopped'};
    const fn = fns[action];
    if (!fn) return;
    applySpoolerButtonState('pending');
    const result = await fn();
    applySpoolerButtonState(result.error ? 'pending' : result.state);
    if (result.error) {
        logStatus('ERR', `Could not ${action} the Print Spooler service: ${result.error}`);
        return;
    }
    logStatus('OK', `Print Spooler service ${pastTense[action]}.`);
}

// --- Write to Flash Drive ---

// Drives from the most recent App.ListRemovableDrives() call, indexed the same
// as the checkboxes rendered from them - same pattern as
// importPrintersCandidates, held here rather than in state since it's only
// ever needed while the modal is open.
let flashDriveCandidates = [];

function formatByteSize(n) {
    if (!n) return '0 B';
    const units = ['B', 'KB', 'MB', 'GB', 'TB'];
    let v = n;
    let i = 0;
    while (v >= 1024 && i < units.length - 1) {
        v /= 1024;
        i++;
    }
    return `${v.toFixed(i === 0 ? 0 : 1)} ${units[i]}`;
}

// flashDriveMode selects what btnFlashDriveConfirm actually does -
// 'write' (the default, opened via the toolbar's own Flash Drive button) is
// the full portable copy (exe + Drivers + Configs + 7-Zip tools) with its
// own Format as exFAT option; 'sync' (opened via the Sync button) is Drivers
// only, no format option at all (formatting an already-in-use flash drive
// makes no sense for a top-up), and runs a Rescan against the destination
// itself afterward. Both share this same modal/drive-checklist rather than
// duplicating it.
let flashDriveMode = 'write';

async function openFlashDriveModal(mode = 'write') {
    flashDriveMode = mode;
    const result = await App.ListRemovableDrives();
    if (result.error) {
        logStatus('ERR', result.error);
        return;
    }
    flashDriveCandidates = result.drives || [];
    const list = el('flashDriveList');
    list.innerHTML = flashDriveCandidates.length === 0
        ? '<p class="modal-hint">No USB flash drives detected.</p>'
        : flashDriveCandidates.map((d, i) => `
            <label class="import-printer-item">
              <input type="checkbox" class="flash-drive-check" data-index="${i}">
              <span class="import-printer-name">${attr(d.letter)}</span>
              <span class="import-printer-detail">${attr(d.label || '(no label)')} - ${formatByteSize(d.freeBytes)} free of ${formatByteSize(d.totalBytes)}</span>
            </label>
        `).join('');

    const isSync = mode === 'sync';
    el('flashDriveTitle').textContent = isSync ? 'Sync Drivers to Flash Drive' : 'Write to Flash Drive';
    el('flashDriveHint').textContent = isSync
        ? 'Copies this computer\'s Drivers folder onto every checked drive (merging into whatever is already there), then extracts anything newly-copied.'
        : 'Writes a portable copy of PDT (this executable, plus its Drivers, Configs, and 7-Zip tools folders) to every checked drive.';
    // Inline style, not the `hidden` attribute/`:not([hidden])` CSS pattern
    // used elsewhere in this file - confirmed live that this row still
    // rendered even with the compiled bundle's own `.hidden = true` logic
    // verified correct (checked the actual embedded JS/CSS byte-for-byte),
    // for a reason that didn't resolve under inspection. Setting `display`
    // directly can't lose to any stylesheet rule regardless of cause.
    el('flashDriveFormatRow').style.display = isSync ? 'none' : '';
    el('flashDriveFormat').checked = false;
    el('btnFlashDriveConfirm').textContent = isSync ? 'Sync' : 'Write';

    el('flashDriveBackdrop').hidden = false;
}

function closeFlashDriveModal() {
    el('flashDriveBackdrop').hidden = true;
}

// Write to Flash Drive: optionally formats the checked drives as exFAT
// (destructive - confirmed separately, by name, right before it happens),
// then writes a portable PDT copy (exe + Drivers + Configs + tools) to
// whichever drives are left. This is the flip side of "install PDT on the
// technician's laptop" - the laptop's own local Drivers/Configs become the
// source for every flash drive stamped out from it.
//
// Sync mode skips formatting entirely (never offered - see
// openFlashDriveModal) and calls SyncDriversToFlashDrives instead of
// WritePortablePDT - Drivers only, no exe/Configs/tools - then RefreshDriverCatalog
// so this running instance's own catalog/no-drivers banner reflect whatever
// just got copied too, not just the flash drive's own copy (which
// SyncDriversToFlashDrives/syncDriversTo already extracted server-side).
async function confirmWriteToFlashDrive() {
    const checks = Array.from(el('flashDriveList').querySelectorAll('.flash-drive-check'));
    const chosen = checks.filter(c => c.checked).map(c => flashDriveCandidates[Number(c.dataset.index)]);
    const mode = flashDriveMode;
    const doFormat = mode === 'write' && el('flashDriveFormat').checked;
    closeFlashDriveModal();
    if (chosen.length === 0) return;

    let letters = chosen.map(d => d.letter);

    if (doFormat) {
        const ok = await showConfirm({
            title: 'Warning',
            message: `This will ERASE ALL DATA on: ${letters.join(', ')}\n\nFormat as exFAT and continue?`,
            okLabel: 'Erase and Format',
        });
        if (!ok) return;
        const formatResult = await App.FormatDrives(letters);
        for (const l of formatResult.succeeded || []) logStatus('OK', `Formatted ${l} as exFAT.`);
        for (const l of Object.keys(formatResult.failed || {})) logStatus('ERR', `Could not format ${l}: ${formatResult.failed[l]}`);
        letters = formatResult.succeeded || [];
        if (letters.length === 0) return;
    } else {
        const ok = await showConfirm(mode === 'sync' ? {
            title: 'Sync Drivers to Flash Drive',
            message: `Sync this computer's Drivers folder to: ${letters.join(', ')}?`,
            okLabel: 'Sync',
        } : {
            title: 'Write to Flash Drive',
            message: `Write a portable copy of PDT (this executable, Drivers, Configs, and 7-Zip tools) to: ${letters.join(', ')}?`,
            okLabel: 'Write',
        });
        if (!ok) return;
    }

    openFlashCopyProgressModal(mode, letters);
    try {
        if (mode === 'sync') {
            const syncResult = await App.SyncDriversToFlashDrives(letters);
            for (const l of syncResult.succeeded || []) logStatus('OK', `Synced Drivers to ${l}.`);
            for (const l of Object.keys(syncResult.failed || {})) logStatus('ERR', `Could not sync Drivers to ${l}: ${syncResult.failed[l]}`);
            if ((syncResult.succeeded || []).length > 0) {
                const status = await App.RefreshDriverCatalog();
                el('noDriversBanner').hidden = status.hasDrivers || !status.ok;
            }
            return;
        }

        const writeResult = await App.WritePortablePDT(letters);
        for (const l of writeResult.succeeded || []) logStatus('OK', `Wrote portable PDT to ${l}.`);
        for (const l of Object.keys(writeResult.failed || {})) logStatus('ERR', `Could not write to ${l}: ${writeResult.failed[l]}`);
    } finally {
        closeFlashCopyProgressModal();
    }
}

// The copy-progress dialog: shown for the entire span of a Write to Flash
// Drive/Sync operation, since a real Drivers folder can be tens of
// thousands of files and take several minutes over a real USB port with
// otherwise zero indication it hadn't just hung (confirmed live). One row
// per letter, each showing whichever step (Drivers/Configs/7-Zip tools) is
// currently copying and a live file-count progress bar -
// updateFlashCopyProgress is wired to the "flashcopy-progress" event once,
// in init(), rather than per-operation, so there's no listener-stacking
// concern across repeated Write/Sync calls; it simply no-ops if the row it
// would update isn't present (the dialog isn't open, or that letter wasn't
// part of the current operation).
function openFlashCopyProgressModal(mode, letters) {
    el('flashCopyProgressTitle').textContent = mode === 'sync' ? 'Syncing Drivers...' : 'Writing to Flash Drive...';
    el('flashCopyProgressList').innerHTML = letters.map(letter => `
        <div class="flash-copy-row" data-letter="${attr(letter)}">
            <div class="flash-copy-row-label">${attr(letter)} - starting...</div>
            <progress class="flash-copy-row-bar" value="0" max="1"></progress>
        </div>
    `).join('');
    el('flashCopyProgressBackdrop').hidden = false;
}

function closeFlashCopyProgressModal() {
    el('flashCopyProgressBackdrop').hidden = true;
}

// formatEta renders a whole number of seconds as a short "~Xm Ys remaining"/
// "~Xs remaining" string. Only called once the backend has actually decided
// an estimate is stable enough to report (see newFlashCopyProgressFunc's own
// doc comment) - there's no "estimating..." placeholder here because
// updateFlashCopyProgress simply omits the whole phrase until then, rather
// than showing a number known to still be unreliable.
function formatEta(seconds) {
    const m = Math.floor(seconds / 60);
    const s = seconds % 60;
    return m > 0 ? `~${m}m ${s}s remaining` : `~${s}s remaining`;
}

function updateFlashCopyProgress(progress) {
    const row = el('flashCopyProgressBackdrop').querySelector(`.flash-copy-row[data-letter="${CSS.escape(progress.letter)}"]`);
    if (!row) return;
    const eta = progress.etaSeconds > 0 ? ` - ${formatEta(progress.etaSeconds)}` : '';
    row.querySelector('.flash-copy-row-label').textContent =
        `${progress.letter} - ${progress.step} (${progress.done} / ${progress.total} files)${eta}`;
    const bar = row.querySelector('.flash-copy-row-bar');
    // Bytes, not file count, drive the bar itself - a file-count percentage
    // is a poor proxy for actual progress once file sizes vary as wildly as
    // a real Drivers folder's do (thousands of tiny files, then one huge
    // installer), the same reason the backend estimates the ETA from bytes
    // too (see CopyProgress's own doc comment).
    bar.max = Math.max(progress.totalBytes, 1);
    bar.value = progress.doneBytes;
}

function openSettingsModal() {
    el('settingsBasePath').value = state.settings.saveFileBasePath;
    el('settingsDriversBasePath').value = state.settings.driversBasePath;
    el('settingsPreinstallBasePath').value = state.settings.preinstallBasePath;
    for (const input of el('settingsSitesPanel').querySelectorAll('.settings-url')) {
        input.value = state.settings.manufacturerUrls?.[input.dataset.mfg] || '';
    }
    renderManufacturerOrderList();
    switchSettingsTab('general');
    el('settingsBackdrop').hidden = false;
}

function closeSettingsModal() {
    el('settingsBackdrop').hidden = true;
}

// Clicking the modal's dim backdrop itself (not the modal box) closes it,
// same as Cancel - but only when the click started and ended on the
// backdrop, so dragging a text selection out over the backdrop before
// releasing doesn't accidentally close it.
function wireBackdropDismiss(backdropId, onClose) {
    let mouseDownOnSelf = false;
    const backdrop = el(backdropId);
    backdrop.addEventListener('mousedown', (e) => {
        mouseDownOnSelf = e.target === e.currentTarget;
    });
    backdrop.addEventListener('click', (e) => {
        if (e.target === e.currentTarget && mouseDownOnSelf) onClose();
    });
}

// In-app replacement for window.confirm() - a native confirm's title bar is
// fixed browser/WebView2 chrome ("wails.localhost says"), which can't be
// removed or reworded from JS/CSS at all, and reads as a broken/unbranded
// popup inside what's otherwise a normal desktop app. Resolves true (OK) or
// false (Cancel/backdrop click). Only one of these is ever open at a time,
// so a single shared modal (and a single pendingConfirmCancel, wired once
// below) is enough - no stacking/queueing needed anywhere this is used.
let pendingConfirmCancel = null;

function showConfirm({title = 'Confirm', message = '', okLabel = 'OK', cancelLabel = 'Cancel'} = {}) {
    return new Promise((resolve) => {
        el('confirmTitle').textContent = title;
        el('confirmMessage').textContent = message;
        const okBtn = el('btnConfirmOk');
        const cancelBtn = el('btnConfirmCancel');
        okBtn.textContent = okLabel;
        cancelBtn.textContent = cancelLabel;

        function cleanup(result) {
            el('confirmBackdrop').hidden = true;
            okBtn.removeEventListener('click', onOk);
            cancelBtn.removeEventListener('click', onCancel);
            pendingConfirmCancel = null;
            resolve(result);
        }
        function onOk() { cleanup(true); }
        function onCancel() { cleanup(false); }
        pendingConfirmCancel = onCancel;
        okBtn.addEventListener('click', onOk);
        cancelBtn.addEventListener('click', onCancel);
        el('confirmBackdrop').hidden = false;
    });
}

// STOP's own dialog - a dedicated 3-way modal (Cancel/Force Stop/Stop)
// rather than another showConfirm(), since showConfirm only ever offers a
// single OK action. Resolves 'cancel', 'graceful', or 'force'. Tracks its
// own pendingStopCancel the same way showConfirm tracks pendingConfirmCancel,
// so a backdrop click cancels instead of leaving the dialog stuck open.
let pendingStopCancel = null;

function showStopDialog() {
    return new Promise((resolve) => {
        const cancelBtn = el('btnStopCancel');
        const forceBtn = el('btnStopForce');
        const gracefulBtn = el('btnStopGraceful');

        function cleanup(result) {
            el('stopBackdrop').hidden = true;
            cancelBtn.removeEventListener('click', onCancel);
            forceBtn.removeEventListener('click', onForce);
            gracefulBtn.removeEventListener('click', onGraceful);
            pendingStopCancel = null;
            resolve(result);
        }
        function onCancel() { cleanup('cancel'); }
        function onForce() { cleanup('force'); }
        function onGraceful() { cleanup('graceful'); }
        pendingStopCancel = onCancel;
        cancelBtn.addEventListener('click', onCancel);
        forceBtn.addEventListener('click', onForce);
        gracefulBtn.addEventListener('click', onGraceful);
        el('stopBackdrop').hidden = false;
    });
}

function switchSettingsTab(tab) {
    for (const btn of document.querySelectorAll('.tab-btn')) {
        btn.classList.toggle('active', btn.dataset.tab === tab);
    }
    for (const panel of document.querySelectorAll('.tab-panel')) {
        panel.hidden = panel.dataset.tabPanel !== tab;
    }
}

async function renderAboutPanel() {
    const info = await App.GetAppInfo();
    el('aboutName').textContent = info.name;
    el('aboutVersion').textContent = info.version;
    el('aboutAuthor').textContent = info.author;
    const link = el('aboutRepoLink');
    link.textContent = info.repoUrl;
    link.addEventListener('click', (e) => {
        e.preventDefault();
        App.OpenRepoURL();
    });

    if (!isMac()) {
        el('aboutSevenZipVersion').textContent = (await App.GetSevenZipVersion()) || 'unavailable';
        el('sevenZipCreditLink').addEventListener('click', (e) => {
            e.preventDefault();
            App.OpenSevenZipHomepage();
        });
    }
}

// The asset URL and version number from the most recent CheckForUpdate
// result with an update available - stashed here rather than re-derived,
// since Update Now needs to hand both straight back to ApplyUpdate without
// asking GitHub again (the version number is what lets ApplyUpdate keep the
// Inno Setup uninstall entry's DisplayVersion - appwiz.cpl's own Version
// column - in sync with the exe it just replaced).
let pendingUpdateAssetUrl = '';
let pendingUpdateVersion = '';

async function checkForUpdate() {
    const btn = el('btnCheckUpdate');
    const status = el('updateStatus');
    btn.disabled = true;
    el('btnApplyUpdate').hidden = true;
    status.textContent = 'Checking...';
    try {
        const result = await App.CheckForUpdate();
        if (result.error) {
            status.textContent = result.error;
        } else if (result.available) {
            status.textContent = `Version ${result.latestVersion} is available (you have ${result.currentVersion}).`;
            pendingUpdateAssetUrl = result.assetUrl;
            pendingUpdateVersion = result.latestVersion;
            el('btnApplyUpdate').hidden = !result.assetUrl;
        } else {
            status.textContent = 'You are running the latest version.';
        }
    } catch (e) {
        status.textContent = `Update check failed: ${e}`;
    } finally {
        btn.disabled = false;
    }
}

async function applyUpdate() {
    const btn = el('btnApplyUpdate');
    btn.disabled = true;
    el('updateStatus').textContent = 'Downloading and installing the update...';
    try {
        const result = await App.ApplyUpdate(pendingUpdateAssetUrl, pendingUpdateVersion);
        if (result.error) {
            el('updateStatus').textContent = result.error;
            btn.disabled = false;
        }
        // On success the app relaunches itself and this process quits - there's
        // nothing further to show here.
    } catch (e) {
        el('updateStatus').textContent = `Update failed: ${e}`;
        btn.disabled = false;
    }
}

// Same pattern as pendingUpdateAssetUrl/checkForUpdate/applyUpdate above,
// just for the bundled 7-Zip tool instead of PDT itself.
let pendingSevenZipAssetUrl = '';

async function checkSevenZipUpdate() {
    const btn = el('btnCheckSevenZipUpdate');
    const status = el('sevenZipUpdateStatus');
    btn.disabled = true;
    el('btnApplySevenZipUpdate').hidden = true;
    status.textContent = 'Checking...';
    try {
        const result = await App.CheckSevenZipUpdate();
        if (result.error) {
            status.textContent = result.error;
        } else if (result.available) {
            status.textContent = `Version ${result.latestVersion} is available (you have ${result.currentVersion}).`;
            pendingSevenZipAssetUrl = result.assetUrl;
            el('btnApplySevenZipUpdate').hidden = !result.assetUrl;
        } else {
            status.textContent = 'You have the latest version of 7-Zip.';
        }
    } catch (e) {
        status.textContent = `Update check failed: ${e}`;
    } finally {
        btn.disabled = false;
    }
}

async function applySevenZipUpdate() {
    const btn = el('btnApplySevenZipUpdate');
    btn.disabled = true;
    el('sevenZipUpdateStatus').textContent = 'Downloading and installing the update...';
    try {
        const result = await App.UpdateSevenZip(pendingSevenZipAssetUrl);
        if (result.error) {
            el('sevenZipUpdateStatus').textContent = result.error;
        } else {
            el('sevenZipUpdateStatus').textContent = 'Updated successfully.';
            el('aboutSevenZipVersion').textContent = (await App.GetSevenZipVersion()) || 'unavailable';
        }
    } catch (e) {
        el('sevenZipUpdateStatus').textContent = `Update failed: ${e}`;
    } finally {
        btn.disabled = false;
    }
}

function wireSettingsModal() {
    renderSettingsSitesPanel();
    renderAboutPanel();

    el('btnSettings').addEventListener('click', openSettingsModal);
    el('btnSettingsCancel').addEventListener('click', closeSettingsModal);
    el('btnCheckUpdate').addEventListener('click', checkForUpdate);
    el('btnApplyUpdate').addEventListener('click', applyUpdate);
    el('btnCheckSevenZipUpdate').addEventListener('click', checkSevenZipUpdate);
    el('btnApplySevenZipUpdate').addEventListener('click', applySevenZipUpdate);

    for (const btn of document.querySelectorAll('.tab-btn')) {
        btn.addEventListener('click', () => switchSettingsTab(btn.dataset.tab));
    }

    wireBackdropDismiss('settingsBackdrop', closeSettingsModal);

    el('btnBrowseBasePath').addEventListener('click', async () => {
        const result = await App.PickFolder(el('settingsBasePath').value);
        if (!result.canceled) el('settingsBasePath').value = result.path;
    });

    el('btnBrowseDriversBasePath').addEventListener('click', async () => {
        const result = await App.PickFolder(el('settingsDriversBasePath').value);
        if (!result.canceled) el('settingsDriversBasePath').value = result.path;
    });

    el('btnBrowsePreinstallBasePath').addEventListener('click', async () => {
        const result = await App.PickFolder(el('settingsPreinstallBasePath').value);
        if (!result.canceled) el('settingsPreinstallBasePath').value = result.path;
    });

    el('btnAlphabetizeMfgOrder').addEventListener('click', (e) => {
        e.preventDefault();
        const sorted = [...currentManufacturerOrder()].sort((a, b) => a.localeCompare(b));
        renderManufacturerOrderList(sorted);
    });

    el('btnSettingsSave').addEventListener('click', async () => {
        const manufacturerUrls = {};
        for (const input of el('settingsSitesPanel').querySelectorAll('.settings-url')) {
            manufacturerUrls[input.dataset.mfg] = input.value;
        }
        const manufacturerOrder = currentManufacturerOrder();
        const saved = await App.SaveSettings({
            saveFileBasePath: el('settingsBasePath').value,
            driversBasePath: el('settingsDriversBasePath').value,
            preinstallBasePath: el('settingsPreinstallBasePath').value,
            manufacturerUrls,
            manufacturerOrder,
        });
        state.settings = saved;
        await refreshManufacturerDropdowns();
        closeSettingsModal();
        logStatus('OK', 'Settings saved. Click Refresh (or restart PDT) for a changed Drivers Base Path to take effect.');
    });

    el('btnCheckUpdates').addEventListener('click', () => {
        App.OpenManufacturerURL(el('defMfg').value);
    });

    // Refresh: rescans the Drivers folder in place (RefreshDriverCatalog),
    // no restart needed - what the no-drivers banner points at once a
    // downloaded driver package has been dropped into the Drivers folder.
    // DriverCandidates/DefaultDriverFor are always called fresh from Go on
    // every dropdown interaction, so the driver lists themselves need no
    // extra refreshing here - only things this snapshots at fetch time
    // (the banner, and the Defaults panel's currently-shown Driver value)
    // need an explicit nudge.
    el('btnRefreshDrivers').addEventListener('click', async () => {
        const btn = el('btnRefreshDrivers');
        btn.disabled = true;
        try {
            const status = await App.RefreshDriverCatalog();
            el('noDriversBanner').hidden = status.hasDrivers || !status.ok;
            if (!status.ok) {
                logStatus('ERR', `Driver catalog refresh failed: ${status.error}`);
            } else {
                const mfgSelect = el('defMfg');
                if (mfgSelect.value) {
                    el('defDriver').value = await App.DefaultDriverFor(mfgSelect.value);
                }
                logStatus('OK', status.hasDrivers
                    ? 'Driver catalog refreshed.'
                    : 'Driver catalog refreshed - still no drivers found in the Drivers folder.');
            }
        } finally {
            btn.disabled = false;
        }
    });

    // Opens the current Drivers Base Path in File Explorer - the same
    // scaffold-then-open call Settings' own right-arrow button uses, just
    // reachable straight from the toolbar without opening Settings first.
    el('btnOpenDriversFolder').addEventListener('click', async () => {
        const result = await App.OpenDriversBasePathInExplorer(state.settings.driversBasePath);
        if (result.error) logStatus('ERR', `Could not open Drivers folder: ${result.error}`);
    });
}

async function deploy() {
    const selected = state.rows.filter(r => r.select);
    if (selected.length === 0) {
        logStatus('WARN', 'No rows checked.');
        return;
    }
    if (state.deploying) return;
    state.deploying = true;
    updateDeployButtonEnabled();
    el('btnStop').disabled = false;
    for (const r of selected) {
        r._failed = false;
        r._succeeded = false;
    }
    // Rows keep their existing red/green classes cleared here via a targeted
    // pass rather than renderGrid(), for the same reason onDeployProgress
    // avoids it - no reason to blow away focus/in-progress edits elsewhere
    // in the grid just to start a deploy.
    for (const r of selected) {
        const tr = document.querySelector(`tr[data-id="${r._id}"]`);
        if (tr) tr.classList.remove('row-failed', 'row-succeeded');
    }
    state.activeDeploy = {rows: selected, nextIndex: 0};
    appendLog([`${formatLogTimestamp(new Date())} [OK] ===== DEPLOYMENT STARTED: ${selected.length} printer(s) =====`]);

    const portPrefix = state.portPrefixEnabled ? state.portPrefixText : '';
    try {
        await App.Deploy(selected.map(rowToPrinterRow), state.salesChainId, portPrefix);
    } finally {
        state.deploying = false;
        state.activeDeploy = null;
        updateDeployButtonEnabled();
        el('btnStop').disabled = true;
        appendLog(['===== DEPLOYMENT COMPLETE ====='].map(l => `${formatLogTimestamp(new Date())} [OK] ${l}`));
        const failed = selected.filter(r => r._failed).length;
        logStatus(failed > 0 ? 'WARN' : 'OK', failed > 0 ? `Deployment finished with ${failed} failure(s).` : 'Deployment finished successfully.');
    }
}

init();
