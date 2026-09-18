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
    salesChainId: 'Used to name saved configuration/Settings files for this job. Letters, numbers, hyphen, and underscore only.',
    manufacturer: 'Printer manufacturer - determines which drivers are offered.',
    driver: 'Driver to install/use for this printer. Type to fuzzy-search; multiple local versions of the same driver appear as separate dated entries - for a model-specific driver name (Kyocera, mainly), typing part of the model narrows the list the same way.',
    model: 'Printer model - optional, free text. Narrows the Driver list the same way typing it there does; a dropdown of known models only appears when that data is actually available (Kyocera, today).',
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
    lpdQueue: 'Optional LPD queue name (macOS deploys only - Windows carries this through for portability but never uses it). Most manufacturers ignore it and respond to any/no queue name; HP ("raw") and Xerox ("lp") are the two known exceptions, auto-filled when you pick that Manufacturer - blank is fine for everyone else.',
    selectAllHeader: 'Check/uncheck every row.',
    saveFileBasePath: 'Where PDT keeps saved JSON configs and captured Settings/Device Settings files, and where Open/Save Configuration start from by default. Defaults to Configs alongside PDT itself when running portably, or a per-user PDT data folder for an installed copy.',
    driversBasePath: 'Where PDT looks for printer drivers (Drivers\\Windows\\<version>\\<Manufacturer>\\... on Windows, Drivers/macOS/<Manufacturer>/<version>/... on macOS) - click Refresh (or restart PDT) after changing this to rescan the new location. Defaults to Drivers alongside PDT itself when running portably, or a per-user PDT data folder for an installed copy.',
    preinstallBasePath: 'Where site-survey "<SaveID> - <Client> - <Address>" subfolders live - Export Configs looks here for the one matching the current Save ID. Stays fixed on this computer regardless of which flash drive PDT is running from. Defaults to Documents/Preinstall under your own user profile - resolved fresh on whichever computer (Windows or macOS) you\'re using, so the same setting works on both. Browse to pick anywhere else instead.',
    manufacturerOrder: 'Drag to reorder - controls the Manufacturer dropdown\'s order in Defaults and in the grid. Settings > Download Centers is always alphabetical regardless of this order.',
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
        select: true, name: '', ip: '', lpdQueueName: '', manufacturer: '', model: '', driver: '',
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
        Name: r.name, IP: r.ip, LPDQueueName: r.lpdQueueName, Manufacturer: r.manufacturer, Model: r.model, Driver: r.driver,
        SNMP: !!r.snmpCommunity, SNMPCommunity: r.snmpCommunity, Mono: r.mono, OneSided: r.oneSided,
        UseExistingPort: r.useExistingPort, AdvancedPrintingFeatures: r.advancedPrintingFeatures,
        DevModeFile: r.devModeFile,
    };
}

function printerRowToRow(pr, select = true) {
    return newRow({
        select, name: pr.Name, ip: pr.IP, lpdQueueName: pr.LPDQueueName || '', manufacturer: pr.Manufacturer, model: pr.Model, driver: pr.Driver,
        snmpCommunity: inferSnmpCommunity(pr.SNMP, pr.SNMPCommunity), mono: pr.Mono, oneSided: pr.OneSided,
        useExistingPort: pr.UseExistingPort, advancedPrintingFeatures: pr.AdvancedPrintingFeatures,
        devModeFile: pr.DevModeFile || '',
    });
}

function rowToSavedRow(r) {
    return {
        Select: r.select, Name: r.name, IP: r.ip, LPDQueueName: r.lpdQueueName, Manufacturer: r.manufacturer, Model: r.model, Driver: r.driver,
        Snmp: !!r.snmpCommunity, SnmpCommunity: r.snmpCommunity, Mono: r.mono, OneSided: r.oneSided,
        UseExistingPort: r.useExistingPort, AdvancedPrintingFeatures: r.advancedPrintingFeatures,
        DevModeFile: r.devModeFile,
    };
}

function savedRowToRow(sr) {
    return newRow({
        select: sr.Select, name: sr.Name, ip: sr.IP, lpdQueueName: sr.LPDQueueName || '', manufacturer: sr.Manufacturer, model: sr.Model, driver: sr.Driver,
        snmpCommunity: inferSnmpCommunity(sr.Snmp, sr.SnmpCommunity), mono: sr.Mono, oneSided: sr.OneSided,
        useExistingPort: sr.UseExistingPort, advancedPrintingFeatures: sr.AdvancedPrintingFeatures,
        devModeFile: sr.DevModeFile || '',
    });
}

// defaultLpdQueueFor: most major MFD brands ignore the LPD device URI's own
// queue-name segment and respond to any/no queue at all - HP ("raw") and
// Xerox ("lp") are the two known exceptions that actually need one. Set
// whenever a row's Manufacturer is picked/changed - both for a fresh row
// (addPrinterRow) and an existing row's own Manufacturer dropdown - so
// LPD-Q always reflects whichever manufacturer is currently selected,
// rather than a stale value left over from whatever it was set to before.
function defaultLpdQueueFor(manufacturer) {
    if (manufacturer === 'HP') return 'raw';
    if (manufacturer === 'Xerox') return 'lp';
    return '';
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
    // Manufacturers with real per-model mac PPD data (Canon today) -
    // fetched once at init() and refreshed alongside the driver catalog
    // (App.MacModelManufacturers, macOS-only - see macModelDriven below).
    // Always empty on Windows, so macModelDriven is unconditionally false
    // there with no extra platform check needed at most call sites.
    macModelManufacturers: new Set(),
    rows: [],
    deploying: false,
    logLines: [],
    settings: {saveFileBasePath: '', driversBasePath: '', manufacturerUrls: {}, directDownloadUrls: {}, manufacturerOrder: []},
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
    <button id="btnExportConfigs" title="Copy this Save ID's Configs files (saved JSON config, captured Settings/Device Settings) to its Preinstall subfolder on this computer.">Export Configs</button>
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
    <button id="btnCloudSync" class="icon-btn-inline" title="Sync this computer's Drivers folder with the team's shared cloud repository - upload new local files, download new files other technicians have added.">
      <svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round" style="vertical-align: middle;">
        <line x1="8" y1="3" x2="8" y2="19"/>
        <polyline points="4,15 8,19 12,15"/>
        <line x1="16" y1="21" x2="16" y2="5"/>
        <polyline points="12,9 16,5 20,9"/>
      </svg>
    </button>
    <button id="btnOpenDriversFolder" class="icon-btn-inline" title="Open Drivers folder">&#128194;</button>
    <span class="catalog-warning" id="catalogWarning" hidden></span>
    <button id="btnSettings" class="icon-btn" title="Settings">&#9881;</button>
  </div>

  <div class="modal-backdrop" id="settingsBackdrop" hidden>
    <div class="modal settings-modal">
      <h3>Settings</h3>
      <div class="tabs">
        <button type="button" class="tab-btn active" data-tab="general">General</button>
        <button type="button" class="tab-btn" data-tab="sites">Download Centers</button>
        <button type="button" class="tab-btn" data-tab="directdownloads">Direct Downloads</button>
        <button type="button" class="tab-btn" data-tab="cloudsync">Cloud Sync</button>
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
          automatically, so "Download Center" in Defaults just opens the page below for the
          selected manufacturer.</p>
        <div id="settingsSitesPanel"></div>
      </div>
      <div class="tab-panel" data-tab-panel="directdownloads" hidden>
        <p class="modal-hint">A real, direct file URL per manufacturer/platform/driver, rather than
          just a general download page - used to automatically fetch or link to the exact driver a
          row's own Driver field resolves to (GitHub issues #18/#15). Only manufacturers with at
          least one configured family are listed; more can be added later.</p>
        <div id="settingsDirectDownloadsPanel"></div>
      </div>
      <div class="tab-panel" data-tab-panel="cloudsync" hidden>
        <p class="modal-hint">Keeps this computer's Drivers folder in sync with the team's shared
          Cloudflare R2 bucket, so every technician benefits from drivers anyone else downloads.</p>
        <label class="modal-field">
          R2 Endpoint
          <input type="text" id="cloudSyncEndpoint" placeholder="https://&lt;account-id&gt;.r2.cloudflarestorage.com">
        </label>
        <label class="modal-field">
          Bucket
          <input type="text" id="cloudSyncBucket" placeholder="pdt">
        </label>
        <label class="modal-field">
          Folder
          <input type="text" id="cloudSyncPrefix" placeholder="Drivers/">
        </label>
        <label class="modal-field">
          Access Key ID
          <input type="text" id="cloudSyncAccessKeyId" autocomplete="off">
        </label>
        <label class="modal-field">
          Secret Access Key
          <input type="password" id="cloudSyncSecretKey" autocomplete="off">
        </label>
        <p class="modal-hint" id="cloudSyncSecretStatus"></p>
        <label class="modal-field" title="How many files Cloud Sync transfers at the same time.">
          Concurrent Transfers
          <input type="number" id="cloudSyncConcurrentTransfers" min="1" max="10" step="1">
        </label>
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
      <label class="modal-field-inline" title="Immediately capture each imported printer's current Settings (print defaults) and Device Settings after import.">
        <input type="checkbox" id="importPrintersGetDevmode" checked> Get Settings
      </label>
      <div class="modal-actions">
        <button id="btnImportPrintersCancel">Cancel</button>
        <button class="primary" id="btnImportPrintersConfirm">Import</button>
      </div>
    </div>
  </div>

  <div class="modal-backdrop" id="exportFolderBackdrop" hidden>
    <div class="modal">
      <h3>Confirm Preinstall Folder</h3>
      <p class="modal-hint">Export Configs will copy this Save ID's files to the Preinstall subfolder below. Confirm it's the right one - an old Save ID's folder can still be sitting there even if you haven't created today's yet.</p>
      <div id="exportFolderList" class="import-printers-list"></div>
      <div class="modal-actions">
        <button id="btnExportFolderCancel">Cancel</button>
        <button class="primary" id="btnExportFolderConfirm">Select</button>
      </div>
    </div>
  </div>

  <div class="modal-backdrop" id="exportFilesBackdrop" hidden>
    <div class="modal">
      <h3>Select Files to Export</h3>
      <p class="modal-hint" id="exportFilesHint"></p>
      <label class="import-printer-item">
        <input type="checkbox" id="exportFilesSelectAll" checked>
        <span class="import-printer-name"><strong>Select All</strong></span>
      </label>
      <div id="exportFilesList" class="import-printers-list"></div>
      <div class="modal-actions">
        <button id="btnExportFilesCancel">Cancel</button>
        <button class="primary" id="btnExportFilesConfirm">Export</button>
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
      <div class="modal-field-inline" id="flashSyncDirectionRow" hidden>
        <label><input type="radio" name="flashSyncDirection" id="flashSyncDirectionTo" value="to" checked> This computer &rarr; Flash Drive</label>
        <label><input type="radio" name="flashSyncDirection" id="flashSyncDirectionFrom" value="from"> Flash Drive &rarr; This computer</label>
      </div>
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
      <div class="modal-actions">
        <button class="danger" id="btnFlashCopyCancel">Cancel</button>
      </div>
    </div>
  </div>

  <div class="modal-backdrop" id="cloudSyncBackdrop" hidden>
    <div class="modal cloud-sync-modal">
      <h3>Cloud Sync</h3>
      <p class="modal-hint" id="cloudSyncHint">Comparing this computer's Drivers folder against the shared cloud repository&hellip;</p>
      <div id="cloudSyncTree" class="cloud-sync-tree"></div>
      <div class="modal-actions">
        <button id="btnCloudSyncClose">Close</button>
        <button class="primary" id="btnCloudSyncConfirm" disabled>Sync Selected</button>
      </div>
    </div>
  </div>

  <div class="modal-backdrop" id="rescanBackdrop" hidden>
    <div class="modal rescan-modal">
      <h3>Rescan Drivers</h3>
      <p class="modal-hint">Pick which manufacturers or driver packages to rescan.</p>
      <div id="rescanTree" class="rescan-tree"></div>
      <div class="modal-actions rescan-actions">
        <button id="btnRescanSelectAll">Select All</button>
        <label class="platform-windows-only" title="Delete the cached .inf files for the selected manufacturer(s)/package(s) first, so they're re-extracted fresh from the archive.">
          <input type="checkbox" id="rescanRemoveInf"> Remove INF
        </label>
      </div>
      <div class="modal-actions">
        <button id="btnRescanClose">Close</button>
        <button class="primary" id="btnRescanConfirm" disabled>Rescan</button>
      </div>
    </div>
  </div>

  <div class="modal-backdrop" id="cloudSyncProgressBackdrop" hidden>
    <div class="modal cloud-sync-progress-modal">
      <h3>Syncing with Cloud...</h3>
      <div class="cloud-sync-current-path" id="cloudSyncCurrentPath"></div>
      <progress class="cloud-sync-current-bar" id="cloudSyncCurrentBar" value="0" max="1"></progress>
      <div class="cloud-sync-current-label" id="cloudSyncCurrentLabel"></div>
      <div class="cloud-sync-queue-label" id="cloudSyncQueueLabel">Up next</div>
      <div id="cloudSyncQueueList" class="cloud-sync-queue-list"></div>
      <progress class="cloud-sync-total-bar" id="cloudSyncTotalBar" value="0" max="1"></progress>
      <div class="cloud-sync-total-label" id="cloudSyncTotalLabel"></div>
      <div class="modal-actions">
        <button id="btnCloudSyncPause">Pause</button>
        <button class="danger" id="btnCloudSyncCancelTransfer">Cancel</button>
      </div>
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
    No printer drivers are installed yet. Pick a Manufacturer below, then click <strong>Download Center</strong> to open its download page - once a driver package is downloaded into the Drivers folder, press the Refresh button (&#128260;) to make it available.
  </div>

  <div class="defaults-panel">
    <fieldset class="defaults-outer">
      <legend>Defaults (used by "Add Printer")</legend>
      <div class="defaults-body">
        <div class="defaults-row">
          <label title="${tip('manufacturer')}">Manufacturer <select id="defMfg" title="${tip('manufacturer')}"></select></label>
          <button type="button" id="btnCheckUpdates" title="Open the selected manufacturer's driver page (configured in Settings &gt; Download Centers).">Download Center</button>
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
      </div>
    </fieldset>
  </div>

  <div class="toolbar">
    <button id="btnAddRow" title="Add a new row using the Defaults above.">Add Printer</button>
    <button id="btnRemoveRow" title="Remove every checked row from the grid.">Remove Selected</button>
    <button id="btnNewCsv" title="Create a blank CSV file with the correct column headers to fill in externally.">New CSV</button>
    <button id="btnImportCsv" title="Import printer rows from a CSV file.">Import CSV</button>
    <button id="btnImportPrinters" class="platform-windows-only" title="Import already-configured printers from this computer.">Import Printers</button>
    <button id="btnGetDevmode" class="platform-windows-only" title="Capture the current Settings (print defaults) and Device Settings from every checked row's local printer.">Get Settings</button>
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
          <th title="${tip('lpdQueue')}">LPD-Q</th>
          <th title="${tip('manufacturer')}">Manufacturer</th>
          <th title="${tip('model')}">Model</th>
          <th title="${tip('driver')}">Driver</th>
          <th class="platform-windows-only" title="${tip('snmpGrid')}">SNMP</th>
          <th title="${tip('mono')}">Mono</th>
          <th title="${tip('oneSided')}">1-side</th>
          <th class="platform-windows-only" title="${tip('useExistingPort')}">UEP</th>
          <th class="platform-windows-only" title="Capture or browse to this row's Settings (print defaults) and Device Settings, applied last during Deploy."></th>
          <th title="Double-click to remove this one row, without needing to check it first."></th>
        </tr>
      </thead>
      <tbody id="gridBody"></tbody>
    </table>
  </div>

  <div class="log-resize-handle" id="logResizeHandle" title="Drag to resize the log panel"></div>

  <div class="log-panel">
    <div class="log-header">
      <span>Log</span>
      <span class="spacer"></span>
      <button id="btnClearLog" title="Clear the log output below.">Clear Log</button>
    </div>
    <pre id="logOutput"></pre>
  </div>

  <div id="logContextMenu" class="context-menu" hidden>
    <button id="logCtxSelectAll" class="context-menu-item">Select All</button>
    <button id="logCtxCopy" class="context-menu-item">Copy</button>
    <div class="context-menu-separator"></div>
    <button id="logCtxClear" class="context-menu-item">Clear Log</button>
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
// The Defaults panel's Subnet field is a *prefix* (3 octets, e.g. "10.1.1."
// - the 4th is filled in per-row), not a full address, so isValidIPv4's own
// 4-octet regex doesn't apply here. Blank is always valid (Subnet is
// optional, not mandatory - stays the default white background, never
// highlighted); a trailing "." is optional too (both "10.1.1" and
// "10.1.1." are fine, matching the field's own placeholder), but anything
// else - the wrong octet count, an octet outside 0-255, a double dot, a
// comma, whitespace - isn't, and gets the same yellow highlight a bad full
// address gets elsewhere.
function isValidSubnetPrefix(s) {
    if (s === '') return true;
    const trimmed = s.endsWith('.') ? s.slice(0, -1) : s;
    const m = /^(\d{1,3})\.(\d{1,3})\.(\d{1,3})$/.exec(trimmed);
    if (!m) return false;
    return m.slice(1).every((part) => {
        if (part.length > 1 && part[0] === '0') return false;
        const n = Number(part);
        return n >= 0 && n <= 255;
    });
}

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
// Download Center button (also job-independent - picking a manufacturer
// and opening its configured download page touches no SalesChain-ID-named
// file, and this is exactly how the no-drivers banner's own bootstrap
// workflow - pick a Manufacturer, Download Center, Refresh - is meant to
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
    const exemptIds = new Set(['btnOpenConfig', 'salesChainId', 'btnSettings', 'btnFlashDrive', 'btnRefreshDrivers', 'btnSyncFlashDrive', 'btnCloudSync', 'btnOpenDriversFolder', 'btnSpooler', 'btnDeploy', 'btnStop', 'defMfg', 'btnCheckUpdates']);
    for (const c of document.querySelectorAll('#app button, #app input, #app select')) {
        if (exemptIds.has(c.id) || c.closest('#settingsBackdrop') || c.closest('#flashDriveBackdrop') || c.closest('#spoolerDropdown') || c.closest('#confirmBackdrop') || c.closest('#cloudSyncBackdrop') || c.closest('#cloudSyncProgressBackdrop') || c.closest('#rescanBackdrop')) continue;
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

// macModelDriven: true for a manufacturer with real per-model mac PPD data
// (Canon today - state.macModelManufacturers, populated from
// App.MacModelManufacturers). Manufacturer, not row-specific, since the
// same question - Defaults panel driver optional? / grid Model mandatory,
// Driver auto-derived from Model? - is asked for both. Always false on
// Windows (state.macModelManufacturers stays empty there).
function macModelDriven(manufacturer) {
    return isMac() && state.macModelManufacturers.has(manufacturer);
}

// applyCatalogStatus is the shared tail end of both the mac one-click
// Refresh Drivers handler and the Windows Rescan dialog's own confirm
// handler - both end up with the exact same CatalogStatus shape
// (App.RefreshDriverCatalog/App.RescanDrivers) and need the exact same UI
// refresh afterward. verb is just "refreshed"/"rescanned" for the OK/ERR log
// line's own wording.
async function applyCatalogStatus(status, verb) {
    el('noDriversBanner').hidden = status.hasDrivers || !status.ok;
    if (!status.ok) {
        logStatus('ERR', `Driver catalog ${verb === 'refreshed' ? 'refresh' : 'rescan'} failed: ${status.error}`);
        return;
    }
    if (isMac()) {
        state.macModelManufacturers = new Set(await App.MacModelManufacturers());
    }
    const mfgSelect = el('defMfg');
    if (mfgSelect.value) {
        el('defDriver').value = await App.DefaultDriverFor(mfgSelect.value);
        el('defDriver').classList.toggle('input-needs-value', !el('defDriver').value && !macModelDriven(mfgSelect.value));
    }
    // A refresh/rescan can change which manufacturers have real per-model
    // mac data - re-render so any already-added row's Model
    // mandatory/optional styling reflects it.
    renderGrid();
    logStatus('OK', status.hasDrivers
        ? `Driver catalog ${verb}.`
        : `Driver catalog ${verb} - still no drivers found in the Drivers folder.`);
    // macOS only - what changed for a manufacturer whose newest package
    // turned out to be different from the one already recorded in its own
    // catalog.<mfg>.json (App.CatalogStatus's own ModelChanges - see
    // driver.DiffModels). Empty on a cache-hit refresh (nothing actually
    // changed) or a first-ever build (nothing to diff against yet).
    for (const change of status.modelChanges || []) {
        logStatus('INFO', change);
    }
}

async function init() {
    state.platform = await App.Platform();
    if (isMac()) {
        state.macModelManufacturers = new Set(await App.MacModelManufacturers());
    }
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

function clearLog() {
    state.logLines = [];
    renderLog();
}

// selectAllLog/copyLog operate on the log's own text content (state.logLines
// joined the same way renderLog itself joins them), not whatever's currently
// selected - "Select All" then "Copy" from the context menu should always
// grab the whole log, matching how a technician would actually want to paste
// it into a support ticket, not just whatever text happened to be selected
// when they right-clicked.
function selectAllLog() {
    const out = el('logOutput');
    const range = document.createRange();
    range.selectNodeContents(out);
    const sel = window.getSelection();
    sel.removeAllRanges();
    sel.addRange(range);
}

async function copyLog() {
    const text = state.logLines.join('\n');
    try {
        await navigator.clipboard.writeText(text);
        return;
    } catch {
        // Falls through to the execCommand fallback below - some webview
        // configurations restrict the async Clipboard API outright.
    }
    const textarea = document.createElement('textarea');
    textarea.value = text;
    textarea.style.position = 'fixed';
    textarea.style.opacity = '0';
    document.body.appendChild(textarea);
    textarea.select();
    try {
        document.execCommand('copy');
    } finally {
        document.body.removeChild(textarea);
    }
}

// wireLogContextMenu replaces the log area's native right-click menu with a
// small custom one (Select All / Copy / Clear Log) - positioned at the
// cursor via "contextmenu", dismissed on an outside click, a menu-item pick,
// or Escape, the same dismiss shape every other transient popup in this file
// (the Spooler dropdown, the Model/Driver combobox) already uses.
function wireLogContextMenu() {
    const menu = el('logContextMenu');

    const hide = () => { menu.hidden = true; };

    el('logOutput').addEventListener('contextmenu', (e) => {
        e.preventDefault();
        menu.hidden = false;
        const maxLeft = window.innerWidth - menu.offsetWidth - 4;
        const maxTop = window.innerHeight - menu.offsetHeight - 4;
        menu.style.left = `${Math.max(0, Math.min(e.clientX, maxLeft))}px`;
        menu.style.top = `${Math.max(0, Math.min(e.clientY, maxTop))}px`;
    });

    document.addEventListener('mousedown', (e) => {
        if (!menu.hidden && !menu.contains(e.target)) hide();
    });
    document.addEventListener('keydown', (e) => {
        if (!menu.hidden && e.key === 'Escape') hide();
    });

    el('logCtxSelectAll').addEventListener('click', () => { hide(); selectAllLog(); });
    el('logCtxCopy').addEventListener('click', () => { hide(); copyLog(); });
    el('logCtxClear').addEventListener('click', () => { hide(); clearLog(); });
}

// Drag-to-resize for the log panel: the handle sits between .grid-wrap
// (flex: 1, so it absorbs whatever height .log-panel doesn't take) and
// .log-panel (fixed height), so growing/shrinking the log panel's height
// is all that's needed to resize both - the grid just fills what's left.
// .log-panel sits flush against the bottom of #app (100vh, nothing below
// it but the handle), so its bottom edge tracks the viewport bottom and
// height = viewport height - pointer Y is exact for the whole drag.
const LOG_PANEL_HEIGHT_KEY = 'pdtLogPanelHeight';
const LOG_PANEL_MIN_HEIGHT = 80;
const GRID_MIN_HEIGHT = 150;

function wireLogResize() {
    const handle = el('logResizeHandle');
    const panel = document.querySelector('.log-panel');

    const saved = parseInt(localStorage.getItem(LOG_PANEL_HEIGHT_KEY), 10);
    if (!isNaN(saved) && saved > 0) {
        panel.style.height = `${saved}px`;
    }

    let dragging = false;

    handle.addEventListener('mousedown', (e) => {
        dragging = true;
        handle.classList.add('dragging');
        document.body.style.cursor = 'row-resize';
        document.body.style.userSelect = 'none';
        e.preventDefault();
    });

    document.addEventListener('mousemove', (e) => {
        if (!dragging) return;
        const maxHeight = Math.max(LOG_PANEL_MIN_HEIGHT, window.innerHeight - GRID_MIN_HEIGHT);
        const newHeight = Math.min(maxHeight, Math.max(LOG_PANEL_MIN_HEIGHT, window.innerHeight - e.clientY));
        panel.style.height = `${newHeight}px`;
    });

    document.addEventListener('mouseup', () => {
        if (!dragging) return;
        dragging = false;
        handle.classList.remove('dragging');
        document.body.style.cursor = '';
        document.body.style.userSelect = '';
        localStorage.setItem(LOG_PANEL_HEIGHT_KEY, panel.style.height.replace('px', ''));
    });
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
// onCommit (optional) fires only from an actual commit - a dropdown item
// picked by click or Enter - never from plain typing, unlike onChange.
// Model's own combobox uses this to auto-fill Driver on macOS: doing that
// from onChange instead fired on every keystroke, including a filter string
// mid-typed and not yet a real model ("5840"), which doesn't fold-match
// anything in the index (see MacModelCandidates) and fell through to
// DriverCandidates' own guess-based fallback - confirmed live, the Driver
// field flashed the manufacturer's single raw guessed package name while
// typing, every time, until a real model was actually picked.
function setupCombobox(input, list, fetchCandidates, onChange, onEnter, onCommit) {
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
        if (onCommit) onCommit(value);
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
      <td><input type="text" class="row-lpdqueue" value="${attr(r.lpdQueueName)}" placeholder="optional" title="${tip('lpdQueue')}"></td>
      <td>${mfgSelectHtml(r)}</td>
      <td><div class="combo"><input type="text" class="row-model${(macModelDriven(r.manufacturer) && !r.model) ? ' input-needs-value' : ''}" value="${attr(r.model)}" title="${tip('model')}"><div class="combo-list" hidden></div></div></td>
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
    const label = set ? 'SETTINGS SET' : 'Get Settings';
    const cls = set ? 'row-devmode set' : 'row-devmode';
    const title = set
        ? `Captured from ${attr(r.devModeFile)}. Click to replace it.`
        : 'Capture this row\'s Settings and Device Settings from a local printer of the same name, or browse to an existing .bin file.';
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
        // No input-needs-value handling here - unlike Name/IP/Driver, LPD-Q
        // is genuinely optional on every manufacturer PDT knows of (see
        // tip('lpdQueue')), so it never gets the yellow needs-value styling.
        tr.querySelector('.row-lpdqueue').addEventListener('input', (e) => { row.lpdQueueName = e.target.value; });
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
            // row.model (typed into the Model field below, or left blank)
            // narrows the candidate list server-side the same way typing
            // part of a model name straight into Driver's own filter text
            // already did - a driver/PPD name that's model-specific already
            // spells the model out (Kyocera's, mainly, on Windows; a macOS
            // PPD's own filename usually does too - see
            // driver.ppdMatchLabel), so both keep working together.
            (filterText) => App.DriverCandidates(row.manufacturer, row.model, filterText),
            (value) => {
                row.driver = value;
                driverInput.classList.toggle('input-needs-value', !value);
            },
            () => addPrinterRow(true),
        );

        const modelCombo = tr.querySelector('.row-model').closest('.combo');
        const modelInput = modelCombo.querySelector('input');
        setupCombobox(
            modelInput,
            modelCombo.querySelector('.combo-list'),
            // Optional field - no data lookup pretending to exist where
            // there's really nothing to search. App.Models exists on both
            // builds (drivercatalog_windows.go/drivercatalog_darwin.go) -
            // Kyocera on Windows, Canon on macOS today, empty for every
            // other manufacturer either way, which just means the dropdown
            // never appears - same "plain free-text input" behavior either
            // way.
            (filterText) => App.Models(row.manufacturer, filterText),
            (value) => {
                row.model = value;
                modelInput.classList.toggle('input-needs-value', macModelDriven(row.manufacturer) && !value);
            },
            () => addPrinterRow(true),
            (value) => {
                // For a manufacturer with real per-model mac data (Canon),
                // Driver is fully derived from Model, not a second thing to
                // pick by hand - DriverCandidates with a blank filterText is
                // already preference-ordered (UFR II first - see
                // MacModelCandidates' own doc comment), so its first result
                // is exactly "the respective model-specific UFR II variant".
                // onCommit, not onChange - a mid-typed filter string ("5840")
                // doesn't fold-match any real model yet, so calling this on
                // every keystroke fell through to DriverCandidates' own
                // guess-based fallback and flashed the manufacturer's single
                // raw guessed package name in Driver until a real model was
                // actually picked (confirmed live - see setupCombobox's own
                // doc comment on onCommit). Windows' own Kyocera model-
                // narrowing is unaffected - this only runs for
                // macModelDriven manufacturers, where the technician was
                // never expected to pick Driver independently of Model at
                // all.
                if (!macModelDriven(row.manufacturer)) return;
                App.DriverCandidates(row.manufacturer, value, '').then((candidates) => {
                    if (row.model !== value) return; // stale - model has since changed again
                    const picked = candidates[0] || '';
                    row.driver = picked;
                    driverInput.value = picked;
                    driverInput.classList.toggle('input-needs-value', !picked);
                });
            },
        );

        const mfgSelect = tr.querySelector('.row-mfg');
        mfgSelect.addEventListener('change', async (e) => {
            row.manufacturer = e.target.value;
            row.model = '';
            // Mirrors addPrinterRow's own "copy the Defaults panel's current
            // Driver" - if the Defaults panel is itself already set to this
            // same manufacturer, its current Driver value (auto-filled or a
            // manual override the technician typed there) is exactly what
            // this row should start with too; otherwise there's nothing
            // relevant to inherit, so ask for that manufacturer's own fresh
            // default the same way the Defaults panel itself would. On
            // macOS this naturally comes out blank for a manufacturer with
            // real per-model data (Canon) - DefaultDriverFor returns "" for
            // those (see its own doc comment) since Driver isn't meaningful
            // again until Model (now mandatory - see modelInput above) is
            // actually picked.
            row.driver = (el('defMfg').value === row.manufacturer) ? el('defDriver').value : await App.DefaultDriverFor(row.manufacturer);
            // Always tracks the newly-picked manufacturer's own default
            // (confirmed live: leaving a stale "raw" behind after switching
            // HP -> Ricoh read as a bug, not a preserved override) - unlike
            // Driver/Model just above, there's no meaningful "this row's own
            // LPD-Q" independent of which manufacturer is currently
            // selected to preserve across a change. Still just a default:
            // typing a custom value afterward isn't touched by anything
            // except picking a (possibly the same) manufacturer again.
            row.lpdQueueName = defaultLpdQueueFor(row.manufacturer);
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

// Per-row Settings button click handler: try a live capture from a local
// printer of the same name first (the reference-machine case this feature
// exists for); if none is found, fall back to browsing for an existing
// .bin file instead (e.g. one captured elsewhere). Re-clicking an
// already-"SETTINGS SET" row replaces it the same way.
async function captureOrBrowseDevMode(row) {
    if (!state.salesChainId) {
        logStatus('WARN', 'Set a Save ID before capturing Settings.');
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
            logStatus('INFO', 'Settings capture canceled.');
            return;
        }
        if (result.error) {
            logStatus('ERR', `Could not capture Settings for "${row.name}": ${result.error}`);
            return;
        }
        row.devModeFile = result.fileName;
        patchRowDevModeButton(row);
        logStatus('OK', `Captured Settings for "${row.name}".`);
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
        // Blank is fine, not "needs a value", for a manufacturer with real
        // per-model mac data (Canon) - the Defaults panel has no Model field
        // to narrow by, so there's no single correct driver to require here
        // (see DefaultDriverFor's own doc comment).
        (value) => { el('defDriver').classList.toggle('input-needs-value', !value && !macModelDriven(el('defMfg').value)); },
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
    el('defDriver').classList.toggle('input-needs-value', !el('defDriver').value && !macModelDriven(mfgSelect.value));
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
    const mfg = el('defMfg').value;
    const row = newRow({
        ip,
        lpdQueueName: defaultLpdQueueFor(mfg),
        manufacturer: mfg,
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

    el('defSubnet').addEventListener('input', (e) => {
        e.target.classList.toggle('input-needs-value', !isValidSubnetPrefix(e.target.value));
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
        el('defDriver').classList.toggle('input-needs-value', !el('defDriver').value && !macModelDriven(el('defMfg').value));
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
            logStatus('WARN', 'Set a Save ID before capturing Settings.');
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
        logStatus('OK', `Captured Settings for ${captured} of ${selected.length} checked row(s).`);
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
    for (const radio of [el('flashSyncDirectionTo'), el('flashSyncDirectionFrom')]) {
        radio.addEventListener('change', () => {
            flashSyncDirection = radio.value;
            renderFlashDriveList();
            updateFlashDriveModalLabels();
        });
    }
    // Cancel immediately stops the in-progress transfer and deletes whatever
    // partial file was mid-copy (see CancelFlashSync/copyTreeMerge) - the
    // await in confirmWriteToFlashDrive resolves/rejects on its own shortly
    // after, closing this modal in its own finally block; disabling the
    // button here just guards against a second click piling on while that's
    // still unwinding.
    el('btnFlashCopyCancel').addEventListener('click', () => {
        App.CancelFlashSync();
        const btn = el('btnFlashCopyCancel');
        btn.disabled = true;
        btn.textContent = 'Canceling...';
    });

    el('btnRescanClose').addEventListener('click', closeRescanModal);
    el('btnRescanConfirm').addEventListener('click', confirmRescan);
    el('btnRescanSelectAll').addEventListener('click', () => {
        const paths = [];
        if (rescanTreeRoot) collectRescanLeafPaths(rescanTreeRoot, paths);
        for (const p of paths) rescanSelection.set(p, true);
        renderRescanTree();
    });
    wireBackdropDismiss('rescanBackdrop', closeRescanModal);
    // Delegated for the same reason as cloudSyncTree's own listener below -
    // renderRescanTree replaces the tree's entire innerHTML on every
    // toggle/checkbox change.
    el('rescanTree').addEventListener('change', (e) => {
        if (e.target.classList.contains('rst-leaf-check')) {
            rescanSelection.set(e.target.dataset.relpath, e.target.checked);
            renderRescanTree();
        } else if (e.target.classList.contains('rst-folder-check')) {
            const node = findRescanNode(e.target.dataset.folderPath);
            const paths = [];
            if (node) collectRescanLeafPaths(node, paths);
            for (const p of paths) rescanSelection.set(p, e.target.checked);
            renderRescanTree();
        }
    });
    el('rescanTree').addEventListener('click', (e) => {
        const btn = e.target.closest('.cst-toggle[data-toggle-path]');
        if (!btn) return;
        const path = btn.dataset.togglePath;
        if (rescanCollapsed.has(path)) {
            rescanCollapsed.delete(path);
        } else {
            rescanCollapsed.add(path);
        }
        renderRescanTree();
    });

    el('btnCloudSync').addEventListener('click', () => openCloudSyncModal());
    el('btnCloudSyncClose').addEventListener('click', closeCloudSyncModal);
    el('btnCloudSyncConfirm').addEventListener('click', confirmCloudSync);
    wireBackdropDismiss('cloudSyncBackdrop', closeCloudSyncModal);
    // Delegated rather than one listener per row - the tree re-renders its
    // entire innerHTML on every toggle/checkbox change (see
    // renderCloudSyncTree), which would otherwise mean re-wiring listeners
    // after every single click.
    el('cloudSyncTree').addEventListener('change', (e) => {
        if (e.target.classList.contains('cst-leaf-check')) {
            cloudSyncSelection.set(e.target.dataset.relpath, e.target.checked);
            saveCloudSyncSelection();
            renderCloudSyncTree();
        } else if (e.target.classList.contains('cst-folder-check')) {
            const node = findCloudSyncNode(e.target.dataset.folderPath);
            const relPaths = [];
            if (node) collectActionableRelPaths(node, relPaths);
            for (const p of relPaths) cloudSyncSelection.set(p, e.target.checked);
            saveCloudSyncSelection();
            renderCloudSyncTree();
        }
    });
    el('cloudSyncTree').addEventListener('click', (e) => {
        const btn = e.target.closest('.cst-toggle[data-toggle-path]');
        if (!btn) return;
        const path = btn.dataset.togglePath;
        if (cloudSyncCollapsed.has(path)) {
            cloudSyncCollapsed.delete(path);
        } else {
            cloudSyncCollapsed.add(path);
        }
        renderCloudSyncTree();
    });
    // Pause/Resume toggles the same button - unlike Cancel, Pause is
    // reversible mid-transfer (see cloudsync.PauseGate's own doc comment),
    // so there's no "disable after clicking" guard here the way Cancel has.
    el('btnCloudSyncPause').addEventListener('click', () => {
        const btn = el('btnCloudSyncPause');
        const nowPaused = btn.dataset.paused !== 'true';
        App.SetCloudSyncPaused(nowPaused);
        btn.dataset.paused = nowPaused ? 'true' : 'false';
        btn.textContent = nowPaused ? 'Resume' : 'Pause';
    });
    el('btnCloudSyncCancelTransfer').addEventListener('click', () => {
        App.CancelCloudSync();
        const btn = el('btnCloudSyncCancelTransfer');
        btn.disabled = true;
        btn.textContent = 'Canceling...';
    });
    EventsOn('cloudsync-progress', (progress) => updateCloudSyncProgress(progress));
    EventsOn('cloudsync-total-progress', (progress) => updateCloudSyncTotalProgress(progress));

    wireBackdropDismiss('confirmBackdrop', () => { if (pendingConfirmCancel) pendingConfirmCancel(); });
    wireBackdropDismiss('stopBackdrop', () => { if (pendingStopCancel) pendingStopCancel(); });

    el('btnClearLog').addEventListener('click', clearLog);

    wireLogContextMenu();
    wireLogResize();

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

// Direct Downloads (GitHub issue #19): unlike the flat, one-URL-per-
// manufacturer Sites panel above, this is a real three-level structure
// (manufacturer -> Windows/macOS -> named driver family -> URL), driven
// entirely by the backend's own driver.directDownloadFamilies table rather
// than anything hardcoded here - so a manufacturer/family added there later
// needs no frontend change at all. Builds structure and fills in each
// field's current value in one pass (unlike renderSettingsSitesPanel,
// called once at startup then re-populated separately on every modal open)
// since this needs a handful of async App calls to even know which fields
// to render at all - see openSettingsModal, its own one caller, for why
// that's safe to await there.
async function renderSettingsDirectDownloadsPanel() {
    const manufacturers = await App.DirectDownloadManufacturers();
    const urls = state.settings.directDownloadUrls || {};
    const platforms = [
        {key: 'Windows', label: 'Windows'},
        {key: 'macOS', label: 'macOS'},
    ];

    const sections = await Promise.all(manufacturers.map(async mfg => {
        const platformBlocks = await Promise.all(platforms.map(async p => {
            const families = await App.DirectDownloadFamiliesFor(mfg, p.key);
            if (families.length === 0) return '';
            const fields = families.map(fam => {
                const current = urls[mfg]?.[p.key]?.[fam] || '';
                return `
                    <label class="modal-field direct-download-field">
                      ${escapeHtml(fam)}
                      <input type="text" class="settings-direct-download-url"
                             data-mfg="${attr(mfg)}" data-platform="${attr(p.key)}" data-family="${attr(fam)}"
                             value="${attr(current)}">
                    </label>`;
            }).join('');
            return `<div class="direct-download-platform">
                      <div class="direct-download-platform-label">${escapeHtml(p.label)}</div>
                      ${fields}
                    </div>`;
        }));
        return `<div class="direct-download-mfg">
                  <div class="direct-download-mfg-name">${escapeHtml(mfg)}</div>
                  ${platformBlocks.join('')}
                </div>`;
    }));

    el('settingsDirectDownloadsPanel').innerHTML = sections.join('');
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
// Settings" is also checked - immediately captures each new row's Settings,
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
        logStatus('WARN', `Imported ${newRows.length} printer(s). Set a Save ID to capture Settings.`);
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
    logStatus('OK', `Imported ${newRows.length} printer(s), captured Settings for ${captured}.`);
}

// --- Export Configs ---

function baseName(path) {
    const parts = path.split(/[\\/]/).filter(Boolean);
    return parts[parts.length - 1] || path;
}

// Shows the Preinstall subfolder(s) matching this Save ID for confirmation
// and resolves to the chosen full path, or null if canceled - always shown,
// even for a single match (Ken's own explicit ask, 2026-09-18): an old Save
// ID's own subfolder can still be sitting there if the tech hasn't created
// today's new one yet, so a single match is never auto-trusted silently.
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

// Shows every Configs file that would be exported, each with its own
// checkbox (all checked by default - Ken's own explicit ask, 2026-09-18:
// exporting everything is the common case, deselecting a file is the
// exception), plus a "select all" checkbox that stays in sync with whether
// every individual box is currently checked (same pattern selectAllHeader
// already uses for the main printer grid). Resolves to the array of
// selected filenames (possibly empty, if the tech unchecks everything), or
// null if canceled.
function pickExportFiles(files) {
    return new Promise((resolve) => {
        el('exportFilesHint').textContent = `${files.length} file(s) found for this Save ID. Uncheck any you don't want to export.`;
        el('exportFilesList').innerHTML = files.map((f, i) => `
            <label class="import-printer-item">
              <input type="checkbox" class="exportFileChoice" value="${i}" checked>
              <span class="import-printer-name">${escapeHtml(f)}</span>
            </label>
        `).join('');
        const selectAll = el('exportFilesSelectAll');
        selectAll.checked = true;
        el('exportFilesBackdrop').hidden = false;

        const fileChoices = () => Array.from(el('exportFilesList').querySelectorAll('.exportFileChoice'));
        function syncSelectAllState() {
            selectAll.checked = fileChoices().every(cb => cb.checked);
        }
        function onFileToggle() { syncSelectAllState(); }
        function onSelectAllToggle(e) {
            for (const cb of fileChoices()) cb.checked = e.target.checked;
        }

        const confirmBtn = el('btnExportFilesConfirm');
        const cancelBtn = el('btnExportFilesCancel');
        function cleanup(result) {
            el('exportFilesBackdrop').hidden = true;
            confirmBtn.removeEventListener('click', onConfirm);
            cancelBtn.removeEventListener('click', onCancel);
            selectAll.removeEventListener('change', onSelectAllToggle);
            el('exportFilesList').removeEventListener('change', onFileToggle);
            resolve(result);
        }
        function onConfirm() {
            cleanup(fileChoices().filter(cb => cb.checked).map(cb => files[Number(cb.value)]));
        }
        function onCancel() { cleanup(null); }
        confirmBtn.addEventListener('click', onConfirm);
        cancelBtn.addEventListener('click', onCancel);
        selectAll.addEventListener('change', onSelectAllToggle);
        el('exportFilesList').addEventListener('change', onFileToggle);
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

    // Always confirmed, even for exactly one match - see pickExportFolder's
    // own doc comment for why.
    const destFolder = await pickExportFolder(listResult.folders);
    if (!destFolder) return;

    const collisionResult = await App.CheckExportCollisions(state.salesChainId, destFolder);
    if (collisionResult.error) {
        logStatus('ERR', collisionResult.error);
        return;
    }
    if (collisionResult.sourceFiles.length === 0) {
        logStatus('WARN', `No Configs files found for Save ID "${state.salesChainId}".`);
        return;
    }

    const selectedFiles = await pickExportFiles(collisionResult.sourceFiles);
    if (!selectedFiles) return;
    if (selectedFiles.length === 0) {
        logStatus('WARN', 'No files selected - nothing exported.');
        return;
    }

    // Only the files actually still selected need an overwrite decision -
    // a deselected file's own collision (if any) is irrelevant now.
    const collidingSelected = collisionResult.colliding.filter(f => selectedFiles.includes(f));
    let mode = 'into';
    if (collidingSelected.length > 0) {
        mode = await resolveExportCollision(collidingSelected);
        if (!mode) return;
    }

    const result = await App.ExportConfigs(state.salesChainId, destFolder, mode, selectedFiles);
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

// flashSyncDirection only matters in 'sync' mode - 'to' (the original,
// still-default direction) copies this computer's own Drivers folder onto
// every checked flash drive; 'from' pulls one flash drive's own Drivers
// folder back onto this computer instead, for picking up whatever another
// technician's own sync run left on a shared drive. 'from' can only ever
// have one real source, unlike 'to' - see renderFlashDriveList, which swaps
// the checklist to single-select (radio buttons) for exactly this direction.
let flashSyncDirection = 'to';

// renderFlashDriveList (re)builds #flashDriveList from flashDriveCandidates
// for the current flashDriveMode/flashSyncDirection - a checkbox list for
// every mode except sync+from, which needs single-select (radio) since
// SyncDriversFromFlashDrive only ever pulls from one drive at a time. Called
// both when the modal first opens and again whenever the direction toggle
// changes, so switching direction doesn't require re-listing drives.
function renderFlashDriveList() {
    const singleSelect = flashDriveMode === 'sync' && flashSyncDirection === 'from';
    const list = el('flashDriveList');
    list.innerHTML = flashDriveCandidates.length === 0
        ? '<p class="modal-hint">No USB flash drives detected.</p>'
        : flashDriveCandidates.map((d, i) => `
            <label class="import-printer-item">
              <input type="${singleSelect ? 'radio' : 'checkbox'}" name="flashDriveChoice" class="flash-drive-check" data-index="${i}">
              <span class="import-printer-name">${attr(d.letter)}</span>
              <span class="import-printer-detail">${attr(d.label || '(no label)')} - ${formatByteSize(d.freeBytes)} free of ${formatByteSize(d.totalBytes)}</span>
            </label>
        `).join('');
}

// updateFlashDriveModalLabels sets the modal's title/hint/confirm-button text
// for the current flashDriveMode/flashSyncDirection - split out from
// openFlashDriveModal so the direction radio's own change handler can update
// these live without re-listing drives.
function updateFlashDriveModalLabels() {
    const isSync = flashDriveMode === 'sync';
    const isFrom = isSync && flashSyncDirection === 'from';
    el('flashDriveTitle').textContent = isFrom ? 'Sync Drivers from Flash Drive' : isSync ? 'Sync Drivers to Flash Drive' : 'Write to Flash Drive';
    el('flashDriveHint').textContent = isFrom
        ? 'Pulls the checked flash drive\'s own Drivers folder onto this computer (merging into whatever is already here) - pick one drive.'
        : isSync
            ? 'Copies this computer\'s Drivers folder onto every checked drive (merging into whatever is already there), then extracts anything newly-copied.'
            : 'Writes a portable copy of PDT (this executable, plus its Drivers, Configs, and 7-Zip tools folders) to every checked drive.';
    el('btnFlashDriveConfirm').textContent = isSync ? 'Sync' : 'Write';
}

async function openFlashDriveModal(mode = 'write') {
    flashDriveMode = mode;
    flashSyncDirection = 'to';
    const result = await App.ListRemovableDrives();
    if (result.error) {
        logStatus('ERR', result.error);
        return;
    }
    flashDriveCandidates = result.drives || [];
    renderFlashDriveList();

    const isSync = mode === 'sync';
    el('flashSyncDirectionTo').checked = true;
    // Inline style, not the `hidden` attribute - same reason
    // flashDriveFormatRow below uses it: confirmed live elsewhere in this
    // modal that toggling `hidden` on a .modal-field-inline row doesn't
    // reliably take effect despite the CSS's own `:not([hidden])` guard.
    el('flashSyncDirectionRow').style.display = isSync ? '' : 'none';
    updateFlashDriveModalLabels();
    // Inline style, not the `hidden` attribute/`:not([hidden])` CSS pattern
    // used elsewhere in this file - confirmed live that this row still
    // rendered even with the compiled bundle's own `.hidden = true` logic
    // verified correct (checked the actual embedded JS/CSS byte-for-byte),
    // for a reason that didn't resolve under inspection. Setting `display`
    // directly can't lose to any stylesheet rule regardless of cause.
    el('flashDriveFormatRow').style.display = isSync ? 'none' : '';
    el('flashDriveFormat').checked = false;

    el('flashDriveBackdrop').hidden = false;
}

function closeFlashDriveModal() {
    el('flashDriveBackdrop').hidden = true;
}

// isCanceledError reports whether a Go error message (from a BatchDriveResult
// failure entry, or a caught rejected promise) is copyTreeMerge's own
// context.Canceled - Go's context package guarantees that exact, stable
// "context canceled" text for it. Used to log a Cancel-button-triggered stop
// as a neutral WARN rather than a red ERR, since it isn't a real failure.
function isCanceledError(message) {
    return typeof message === 'string' && message.includes('context canceled');
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
//
// Sync mode's own 'from' direction (flashSyncDirection) reverses the copy -
// SyncDriversFromFlashDrive instead of SyncDriversToFlashDrives - pulling
// exactly one checked drive's Drivers folder back onto this computer, for
// picking up whatever a different technician's own sync added to a shared
// drive since this laptop last saw it.
async function confirmWriteToFlashDrive() {
    const mode = flashDriveMode;
    const direction = mode === 'sync' ? flashSyncDirection : 'to';
    const checks = Array.from(el('flashDriveList').querySelectorAll('.flash-drive-check'));
    const chosen = checks.filter(c => c.checked).map(c => flashDriveCandidates[Number(c.dataset.index)]);
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
        const ok = await showConfirm(direction === 'from' ? {
            title: 'Sync Drivers from Flash Drive',
            message: `Sync ${letters[0]}'s own Drivers folder onto this computer?`,
            okLabel: 'Sync',
        } : mode === 'sync' ? {
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
        if (direction === 'from') {
            const letter = letters[0];
            try {
                await App.SyncDriversFromFlashDrive(letter);
                logStatus('OK', `Synced Drivers from ${letter}.`);
                const status = await App.RefreshDriverCatalog();
                el('noDriversBanner').hidden = status.hasDrivers || !status.ok;
            } catch (err) {
                const msg = err && err.message ? err.message : String(err);
                if (isCanceledError(msg)) {
                    logStatus('WARN', `Sync from ${letter} canceled.`);
                } else {
                    logStatus('ERR', `Could not sync Drivers from ${letter}: ${msg}`);
                }
            }
            return;
        }

        if (mode === 'sync') {
            const syncResult = await App.SyncDriversToFlashDrives(letters);
            for (const l of syncResult.succeeded || []) logStatus('OK', `Synced Drivers to ${l}.`);
            for (const l of Object.keys(syncResult.failed || {})) {
                const msg = syncResult.failed[l];
                logStatus(isCanceledError(msg) ? 'WARN' : 'ERR', isCanceledError(msg) ? `Sync to ${l} canceled.` : `Could not sync Drivers to ${l}: ${msg}`);
            }
            if ((syncResult.succeeded || []).length > 0) {
                const status = await App.RefreshDriverCatalog();
                el('noDriversBanner').hidden = status.hasDrivers || !status.ok;
            }
            return;
        }

        const writeResult = await App.WritePortablePDT(letters);
        for (const l of writeResult.succeeded || []) logStatus('OK', `Wrote portable PDT to ${l}.`);
        for (const l of Object.keys(writeResult.failed || {})) {
            const msg = writeResult.failed[l];
            logStatus(isCanceledError(msg) ? 'WARN' : 'ERR', isCanceledError(msg) ? `Write to ${l} canceled.` : `Could not write to ${l}: ${msg}`);
        }
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
    // Reset from whatever state a previous run's own Cancel click may have
    // left the button in (disabled, "Canceling...") - each fresh run gets
    // its own fresh Cancel button.
    const cancelBtn = el('btnFlashCopyCancel');
    cancelBtn.disabled = false;
    cancelBtn.textContent = 'Cancel';
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

// ---- Cloud Sync ----
//
// cloudSyncItems: the flat plan GetCloudSyncPlan returned (one entry per
// relative path on either side). cloudSyncSelection: relPath -> checked,
// but only for actionable (upload/download) items - a conflict has no
// checkbox at all (see renderCloudSyncLeaf), so it's never in this map.
// cloudSyncCollapsed: folder paths currently collapsed, in-memory only for
// this one modal session (not persisted - unlike the checkbox selection
// itself, which is). cloudSyncTreeRoot: the nested tree renderCloudSyncTree
// last built from cloudSyncItems, kept around so checkbox/toggle click
// handlers can look a folder's own node back up by path without rebuilding
// the whole tree on every click.
let cloudSyncItems = [];
let cloudSyncSelection = new Map();
let cloudSyncCollapsed = new Set();
let cloudSyncTreeRoot = null;

function buildCloudSyncTree(items) {
    const root = { name: '', path: '', children: new Map(), item: null };
    for (const item of items) {
        const parts = item.relPath.split('/');
        let node = root;
        let pathSoFar = '';
        for (let i = 0; i < parts.length; i++) {
            pathSoFar = pathSoFar ? `${pathSoFar}/${parts[i]}` : parts[i];
            if (!node.children.has(parts[i])) {
                node.children.set(parts[i], { name: parts[i], path: pathSoFar, children: new Map(), item: null });
            }
            node = node.children.get(parts[i]);
            if (i === parts.length - 1) node.item = item;
        }
    }
    return root;
}

function findCloudSyncNode(path) {
    let node = cloudSyncTreeRoot;
    for (const part of path.split('/')) {
        node = node?.children.get(part);
        if (!node) return null;
    }
    return node;
}

// collectActionableRelPaths gathers every upload/download leaf's own
// relPath under node (a folder or a single leaf) into out - a conflict leaf
// is deliberately never included, since there's nothing safe to check it
// into: Sync must never guess which side of a size mismatch is right (see
// cloudsync.ActionConflict's own Go doc comment).
function collectActionableRelPaths(node, out) {
    if (node.item) {
        if (node.item.action === 'upload' || node.item.action === 'download') out.push(node.item.relPath);
        return;
    }
    for (const child of node.children.values()) collectActionableRelPaths(child, out);
}

// folderCheckState is a folder's own tri-state checkbox value, derived
// entirely from its actionable descendants' current selection - null (no
// checkbox shown at all) for a folder with nothing actionable under it
// (every descendant already synced, or every descendant a conflict).
function folderCheckState(node) {
    const relPaths = [];
    collectActionableRelPaths(node, relPaths);
    if (relPaths.length === 0) return null;
    const checkedCount = relPaths.filter(p => cloudSyncSelection.get(p)).length;
    if (checkedCount === 0) return 'unchecked';
    if (checkedCount === relPaths.length) return 'checked';
    return 'indeterminate';
}

// cloudSyncActionLabel used to fall through to "Conflict" for any action
// that wasn't literally "upload" or "download" - a real bug found live: it
// was written back when only those two plus "conflict" ever reached here,
// but GetCloudSyncPlan actually returns every item BuildPlan produces,
// "synced" included, so an already-matching file (nothing wrong with it at
// all) got mislabeled with the same alarming red "Conflict" badge as a
// genuine mismatch - confirmed by the dialog's own hint text right above the
// tree ("Nothing to sync...") only ever showing when the real conflict count
// is zero, which is exactly what was happening.
function cloudSyncActionLabel(action) {
    if (action === 'upload') return { text: '↑ Upload', cls: 'cst-action-upload' };
    if (action === 'download') return { text: '↓ Download', cls: 'cst-action-download' };
    if (action === 'synced') return { text: '✓ Synced', cls: 'cst-action-synced' };
    return { text: '⚠ Conflict', cls: 'cst-action-conflict' };
}

function renderCloudSyncLeaf(item) {
    const actionable = item.action === 'upload' || item.action === 'download';
    const spacerTitle = item.action === 'synced'
        ? 'Already in sync on both sides - nothing to do.'
        : 'Sizes differ on each side - this needs to be looked at by hand before it can sync either direction';
    const checkboxHtml = actionable
        ? `<input type="checkbox" class="cst-leaf-check" data-relpath="${attr(item.relPath)}" ${cloudSyncSelection.get(item.relPath) ? 'checked' : ''}>`
        : `<span class="cst-checkbox-spacer" title="${attr(spacerTitle)}"></span>`;
    const { text: actionText, cls: actionCls } = cloudSyncActionLabel(item.action);
    const sizeText = item.action === 'conflict'
        ? `${formatByteSize(item.localSize)} / ${formatByteSize(item.remoteSize)}`
        : formatByteSize(item.action === 'upload' ? item.localSize : item.remoteSize);
    const name = item.relPath.split('/').pop();
    return `
        <div class="cst-row ${item.isNew ? 'cst-new' : ''}" title="${attr(item.relPath)}">
          <span class="cst-toggle"></span>
          ${checkboxHtml}
          ${item.isNew ? '<span class="cst-new-dot" title="New since you last opened Cloud Sync"></span>' : ''}
          <span class="cst-name">${attr(name)}</span>
          <span class="cst-size">${sizeText}</span>
          <span class="cst-action ${actionCls}">${actionText}</span>
        </div>
    `;
}

function renderCloudSyncEntry(node) {
    if (node.item) {
        return renderCloudSyncLeaf(node.item);
    }
    const state = folderCheckState(node);
    const checkboxHtml = state === null
        ? '<span class="cst-checkbox-spacer"></span>'
        : `<input type="checkbox" class="cst-folder-check" data-folder-path="${attr(node.path)}" ${state === 'checked' ? 'checked' : ''} data-indeterminate="${state === 'indeterminate' ? '1' : ''}">`;
    const collapsed = cloudSyncCollapsed.has(node.path);
    const entries = Array.from(node.children.values()).sort((a, b) => {
        const aFolder = !a.item, bFolder = !b.item;
        return aFolder !== bFolder ? (aFolder ? -1 : 1) : a.name.localeCompare(b.name);
    });
    return `
        <div class="cst-node">
          <div class="cst-row">
            <button type="button" class="cst-toggle" data-toggle-path="${attr(node.path)}">${collapsed ? '▸' : '▾'}</button>
            ${checkboxHtml}
            <span class="cst-name">${attr(node.name)}</span>
          </div>
          <div class="cst-children" ${collapsed ? 'hidden' : ''}>
            ${entries.map(renderCloudSyncEntry).join('')}
          </div>
        </div>
    `;
}

function renderCloudSyncTree() {
    cloudSyncTreeRoot = buildCloudSyncTree(cloudSyncItems);
    const container = el('cloudSyncTree');
    if (cloudSyncItems.length === 0) {
        container.innerHTML = '<p class="modal-hint">Nothing to sync - this computer and the cloud repository already match.</p>';
    } else {
        const entries = Array.from(cloudSyncTreeRoot.children.values()).sort((a, b) => {
            const aFolder = !a.item, bFolder = !b.item;
            return aFolder !== bFolder ? (aFolder ? -1 : 1) : a.name.localeCompare(b.name);
        });
        container.innerHTML = entries.map(renderCloudSyncEntry).join('');
    }
    // indeterminate is a DOM property, not an HTML attribute - set in a pass
    // after the markup above is actually attached to the document (setting
    // it on a detached element, or via an attribute in the HTML string
    // itself, has no effect).
    for (const cb of container.querySelectorAll('.cst-folder-check')) {
        cb.indeterminate = cb.dataset.indeterminate === '1';
    }
    el('btnCloudSyncConfirm').disabled = !Array.from(cloudSyncSelection.values()).some(v => v);
}

function saveCloudSyncSelection() {
    const deselected = [];
    for (const [relPath, selected] of cloudSyncSelection) {
        if (!selected) deselected.push(relPath);
    }
    App.SaveCloudSyncSelection(deselected).catch(err => {
        logStatus('ERR', `Could not save Cloud Sync selection: ${err && err.message ? err.message : err}`);
    });
}

async function openCloudSyncModal() {
    el('cloudSyncBackdrop').hidden = false;
    el('cloudSyncHint').textContent = 'Comparing this computer\'s Drivers folder against the shared cloud repository…';
    el('cloudSyncTree').innerHTML = '';
    el('btnCloudSyncConfirm').disabled = true;

    const result = await App.GetCloudSyncPlan();
    if (result.error) {
        el('cloudSyncHint').textContent = result.error;
        return;
    }
    cloudSyncItems = result.items || [];
    cloudSyncSelection = new Map();
    for (const item of cloudSyncItems) {
        if (item.action === 'upload' || item.action === 'download') {
            cloudSyncSelection.set(item.relPath, item.selected);
        }
    }
    const actionable = cloudSyncItems.filter(i => i.action === 'upload' || i.action === 'download').length;
    const conflicts = cloudSyncItems.filter(i => i.action === 'conflict').length;
    el('cloudSyncHint').textContent = actionable === 0 && conflicts === 0
        ? 'Nothing to sync - this computer and the cloud repository already match.'
        : `${actionable} file${actionable === 1 ? '' : 's'} to sync` +
          (conflicts > 0 ? `, ${conflicts} conflict${conflicts === 1 ? '' : 's'} need manual review` : '') + '.';
    renderCloudSyncTree();
}

function closeCloudSyncModal() {
    el('cloudSyncBackdrop').hidden = true;
}

// ---- Rescan (GitHub issue #10 - Windows only, see btnRefreshDrivers) ----
//
// A simpler two-level version of the Cloud Sync tree above: manufacturer
// rows, each expandable to its own individual driver packages
// (App.ListRescanTargets), with the exact same tri-state folder-checkbox
// pattern (folderCheckState/collectActionableRelPaths there) reused here as
// rescanFolderCheckState/collectRescanLeafPaths - no size/action column
// needed, since every leaf is just "included in this rescan or not". Leaf
// paths are "Manufacturer/RelPath" strings, matching exactly what
// App.RescanDrivers' own removeInf scope expects
// (driver.RemoveInfCacheForSelection) - no separate encode/decode step.
let rescanTargets = [];
let rescanSelection = new Map(); // "Manufacturer/RelPath" -> checked
let rescanCollapsed = new Set();
let rescanTreeRoot = null;

function buildRescanTree(targets) {
    const root = { name: '', path: '', children: new Map(), leaf: false };
    for (const mfg of targets) {
        const mfgNode = { name: mfg.name, path: mfg.name, children: new Map(), leaf: false };
        root.children.set(mfg.name, mfgNode);
        for (const pkg of mfg.packages || []) {
            const leafPath = `${mfg.name}/${pkg.relPath}`;
            mfgNode.children.set(leafPath, { name: pkg.name, path: leafPath, children: new Map(), leaf: true });
        }
    }
    return root;
}

function findRescanNode(path) {
    let node = rescanTreeRoot;
    for (const part of path.split('/')) {
        node = node?.children.get(part);
        if (!node) return null;
    }
    return node;
}

function collectRescanLeafPaths(node, out) {
    if (node.leaf) {
        out.push(node.path);
        return;
    }
    for (const child of node.children.values()) collectRescanLeafPaths(child, out);
}

function rescanFolderCheckState(node) {
    const paths = [];
    collectRescanLeafPaths(node, paths);
    if (paths.length === 0) return null;
    const checkedCount = paths.filter(p => rescanSelection.get(p)).length;
    if (checkedCount === 0) return 'unchecked';
    if (checkedCount === paths.length) return 'checked';
    return 'indeterminate';
}

function renderRescanEntry(node) {
    if (node.leaf) {
        return `
            <div class="cst-row" title="${attr(node.path)}">
              <span class="cst-toggle"></span>
              <input type="checkbox" class="rst-leaf-check" data-relpath="${attr(node.path)}" ${rescanSelection.get(node.path) ? 'checked' : ''}>
              <span class="cst-name">${attr(node.name)}</span>
            </div>
        `;
    }
    const state = rescanFolderCheckState(node);
    const checkboxHtml = state === null
        ? '<span class="cst-checkbox-spacer"></span>'
        : `<input type="checkbox" class="rst-folder-check" data-folder-path="${attr(node.path)}" ${state === 'checked' ? 'checked' : ''} data-indeterminate="${state === 'indeterminate' ? '1' : ''}">`;
    const collapsed = rescanCollapsed.has(node.path);
    const entries = Array.from(node.children.values()).sort((a, b) => a.name.localeCompare(b.name));
    return `
        <div class="cst-node">
          <div class="cst-row">
            <button type="button" class="cst-toggle" data-toggle-path="${attr(node.path)}">${collapsed ? '▸' : '▾'}</button>
            ${checkboxHtml}
            <span class="cst-name">${attr(node.name)}</span>
          </div>
          <div class="cst-children" ${collapsed ? 'hidden' : ''}>
            ${entries.map(renderRescanEntry).join('')}
          </div>
        </div>
    `;
}

function renderRescanTree() {
    rescanTreeRoot = buildRescanTree(rescanTargets);
    const container = el('rescanTree');
    if (rescanTargets.length === 0) {
        container.innerHTML = '<p class="modal-hint">No driver packages found in the Drivers folder.</p>';
    } else {
        const entries = Array.from(rescanTreeRoot.children.values()).sort((a, b) => a.name.localeCompare(b.name));
        container.innerHTML = entries.map(renderRescanEntry).join('');
    }
    for (const cb of container.querySelectorAll('.rst-folder-check')) {
        cb.indeterminate = cb.dataset.indeterminate === '1';
    }
    el('btnRescanConfirm').disabled = !Array.from(rescanSelection.values()).some(v => v);
}

async function openRescanModal() {
    el('rescanBackdrop').hidden = false;
    el('rescanTree').innerHTML = '';
    el('rescanRemoveInf').checked = false;
    el('btnRescanConfirm').disabled = true;
    rescanSelection = new Map();

    const targets = await App.ListRescanTargets();
    rescanTargets = targets || [];
    renderRescanTree();
}

function closeRescanModal() {
    el('rescanBackdrop').hidden = true;
}

async function confirmRescan() {
    const selected = Array.from(rescanSelection.entries()).filter(([, checked]) => checked).map(([p]) => p);
    if (selected.length === 0) return;
    const btn = el('btnRescanConfirm');
    btn.disabled = true;
    logStatus('INFO', 'Rescanning driver catalog...');
    try {
        const status = await App.RescanDrivers(selected, el('rescanRemoveInf').checked);
        closeRescanModal();
        await applyCatalogStatus(status, 'rescanned');
    } finally {
        btn.disabled = false;
    }
}

// formatRate renders a bytes-per-second number as e.g. "4.2 MB/s" (reusing
// formatByteSize's own unit logic) - "" for a not-yet-known/zero rate,
// rather than a misleading "0 B/s" flashing at the very start of a batch
// before the backend's own decayed rate estimator has anything to report.
function formatRate(bytesPerSec) {
    if (!bytesPerSec || bytesPerSec <= 0) return '';
    return `${formatByteSize(bytesPerSec)}/s`;
}

// ---- Cloud Sync progress dialog ----
//
// A rolling single "spotlight" file (the longest-running transfer still in
// progress) gets the big bar/ETA/path treatment at the top; every other
// concurrently-transferring file (Settings' own Concurrent Transfers can be
// more than 1) shows in the queue list below with its own small inline
// bar, alongside everything not yet started; the whole batch's own combined
// progress/rate/ETA sits at the bottom (see SyncCloud/CloudSyncTotalProgress,
// Go). cloudSyncQueueOrder is the batch's own submitted order - queue
// position for anything not yet started; cloudSyncProgressByPath/
// cloudSyncStartedAt/cloudSyncDone are all keyed by relPath and rebuilt
// fresh each time openCloudSyncProgressModal runs.
let cloudSyncQueueOrder = [];
let cloudSyncProgressByPath = new Map(); // relPath -> {done, total, direction, etaSeconds}
let cloudSyncStartedAt = new Map(); // relPath -> dispatch sequence number, set on that file's first progress event
let cloudSyncSeq = 0;
let cloudSyncDone = new Set(); // relPaths whose last progress event reported done >= total

function openCloudSyncProgressModal(relPaths) {
    cloudSyncQueueOrder = relPaths;
    cloudSyncProgressByPath = new Map();
    cloudSyncStartedAt = new Map();
    cloudSyncSeq = 0;
    cloudSyncDone = new Set();

    const pauseBtn = el('btnCloudSyncPause');
    pauseBtn.disabled = false;
    pauseBtn.textContent = 'Pause';
    pauseBtn.dataset.paused = 'false';
    const cancelBtn = el('btnCloudSyncCancelTransfer');
    cancelBtn.disabled = false;
    cancelBtn.textContent = 'Cancel';

    el('cloudSyncTotalBar').value = 0;
    el('cloudSyncTotalLabel').textContent = '';
    renderCloudSyncProgress();
    el('cloudSyncProgressBackdrop').hidden = false;
}

function closeCloudSyncProgressModal() {
    el('cloudSyncProgressBackdrop').hidden = true;
}

function renderCloudSyncProgress() {
    const active = cloudSyncQueueOrder
        .filter(p => cloudSyncStartedAt.has(p) && !cloudSyncDone.has(p))
        .sort((a, b) => cloudSyncStartedAt.get(a) - cloudSyncStartedAt.get(b));
    const waiting = cloudSyncQueueOrder.filter(p => !cloudSyncStartedAt.has(p) && !cloudSyncDone.has(p));

    const spotlightPath = active[0];
    if (spotlightPath) {
        const p = cloudSyncProgressByPath.get(spotlightPath);
        const dirLabel = p.direction === 'upload' ? 'Uploading' : 'Downloading';
        const pct = p.total > 0 ? Math.round((p.done / p.total) * 100) : 0;
        const eta = p.etaSeconds > 0 ? ` - ${formatEta(p.etaSeconds)}` : '';
        el('cloudSyncCurrentPath').textContent = spotlightPath;
        el('cloudSyncCurrentPath').title = spotlightPath;
        const bar = el('cloudSyncCurrentBar');
        bar.max = Math.max(p.total, 1);
        bar.value = p.done;
        el('cloudSyncCurrentLabel').textContent =
            `${dirLabel} ${spotlightPath.split('/').pop()} - ${formatByteSize(p.done)} / ${formatByteSize(p.total)} (${pct}%)${eta}`;
    } else {
        el('cloudSyncCurrentPath').textContent = waiting.length > 0 ? 'Preparing next file…' : '';
        el('cloudSyncCurrentPath').title = '';
        el('cloudSyncCurrentBar').value = 0;
        el('cloudSyncCurrentLabel').textContent = '';
    }

    const queueEntries = [...active.slice(1), ...waiting];
    el('cloudSyncQueueLabel').textContent = queueEntries.length > 0 ? `Up next (${queueEntries.length})` : '';
    el('cloudSyncQueueList').innerHTML = queueEntries.map(relPath => {
        const name = relPath.split('/').pop();
        const p = cloudSyncProgressByPath.get(relPath);
        if (p) {
            const pct = p.total > 0 ? Math.round((p.done / p.total) * 100) : 0;
            return `
                <div class="cloud-sync-queue-row cloud-sync-queue-row-active" title="${attr(relPath)}">
                    <span class="cloud-sync-queue-name">${attr(name)}</span>
                    <progress class="cloud-sync-queue-bar" value="${p.done}" max="${Math.max(p.total, 1)}"></progress>
                    <span class="cloud-sync-queue-pct">${pct}%</span>
                </div>
            `;
        }
        return `
            <div class="cloud-sync-queue-row" title="${attr(relPath)}">
                <span class="cloud-sync-queue-name">${attr(name)}</span>
            </div>
        `;
    }).join('');
}

function updateCloudSyncProgress(progress) {
    if (!cloudSyncQueueOrder.includes(progress.relPath)) return;
    cloudSyncProgressByPath.set(progress.relPath, {
        done: progress.done, total: progress.total, direction: progress.direction, etaSeconds: progress.etaSeconds,
    });
    if (!cloudSyncStartedAt.has(progress.relPath)) {
        cloudSyncStartedAt.set(progress.relPath, cloudSyncSeq++);
    }
    if (progress.total > 0 && progress.done >= progress.total) {
        cloudSyncDone.add(progress.relPath);
    }
    renderCloudSyncProgress();
}

function updateCloudSyncTotalProgress(progress) {
    const bar = el('cloudSyncTotalBar');
    bar.max = Math.max(progress.totalBytes, 1);
    bar.value = progress.doneBytes;
    const pct = progress.totalBytes > 0 ? Math.round((progress.doneBytes / progress.totalBytes) * 100) : 0;
    const rateText = formatRate(progress.rateBytesPerSec);
    const eta = progress.etaSeconds > 0 ? ` - ${formatEta(progress.etaSeconds)}` : '';
    el('cloudSyncTotalLabel').textContent =
        `Total: ${formatByteSize(progress.doneBytes)} / ${formatByteSize(progress.totalBytes)} (${pct}%)` +
        (rateText ? ` - ${rateText}` : '') + eta;
}

// confirmCloudSync mirrors confirmWriteToFlashDrive's own shape (open the
// progress modal, await the batch call, log per-item results, refresh the
// catalog on success, close the progress modal in a finally) - see
// SyncCloud's own Go doc comment for why the paths sent here are only a
// request, not a guarantee: a path whose Action changed (already synced, or
// now a conflict) by the time SyncCloud actually runs is silently skipped
// rather than acted on with stale information.
async function confirmCloudSync() {
    const selectedPaths = Array.from(cloudSyncSelection.entries()).filter(([, sel]) => sel).map(([p]) => p);
    if (selectedPaths.length === 0) return;
    closeCloudSyncModal();
    openCloudSyncProgressModal(selectedPaths);
    try {
        const result = await App.SyncCloud(selectedPaths);
        for (const p of result.succeeded || []) logStatus('OK', `Synced ${p} with the cloud.`);
        for (const p of Object.keys(result.failed || {})) {
            const msg = result.failed[p];
            logStatus(isCanceledError(msg) ? 'WARN' : 'ERR', isCanceledError(msg) ? `Cloud sync of ${p} canceled.` : `Could not sync ${p}: ${msg}`);
        }
        if ((result.succeeded || []).length > 0) {
            const status = await App.RefreshDriverCatalog();
            el('noDriversBanner').hidden = status.hasDrivers || !status.ok;
        }
    } finally {
        closeCloudSyncProgressModal();
    }
}

async function openSettingsModal() {
    el('settingsBasePath').value = state.settings.saveFileBasePath;
    el('settingsDriversBasePath').value = state.settings.driversBasePath;
    el('settingsPreinstallBasePath').value = state.settings.preinstallBasePath;
    for (const input of el('settingsSitesPanel').querySelectorAll('.settings-url')) {
        input.value = state.settings.manufacturerUrls?.[input.dataset.mfg] || '';
    }
    await renderSettingsDirectDownloadsPanel();
    renderManufacturerOrderList();
    const cs = state.settings.cloudSync || {};
    el('cloudSyncEndpoint').value = cs.endpoint || '';
    el('cloudSyncBucket').value = cs.bucket || '';
    el('cloudSyncPrefix').value = cs.prefix || '';
    el('cloudSyncAccessKeyId').value = cs.accessKeyId || '';
    // Secret Access Key is never sent back from the backend (see
    // Settings.CloudSyncSecretKey's own doc comment) - this field always
    // opens empty; leaving it empty on Save means "keep whatever's already
    // stored," typing a new value replaces it.
    el('cloudSyncSecretKey').value = '';
    el('cloudSyncSecretKey').placeholder = state.settings.cloudSyncHasSecret ? 'Already set - leave blank to keep it' : 'Not set';
    el('cloudSyncSecretStatus').textContent = state.settings.cloudSyncHasSecret
        ? 'A Secret Access Key is already stored in this computer\'s keychain.'
        : 'No Secret Access Key stored yet - Cloud Sync won\'t work until one is saved here.';
    el('cloudSyncConcurrentTransfers').value = cs.concurrentTransfers || 3;
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

    // Each Browse button's own App.PickFolder call can reject (e.g. Wails'
    // own runtime.OpenDirectoryDialog refuses to even open when handed a
    // starting directory that no longer exists - see app.go's own
    // nearestExistingDir, which already guards against the one real way
    // that happened live) - caught and logged rather than left to fail
    // silently, since an uncaught rejection here previously looked exactly
    // like "the button does nothing at all" with no visible sign of why.
    el('btnBrowseBasePath').addEventListener('click', async () => {
        try {
            const result = await App.PickFolder(el('settingsBasePath').value);
            if (!result.canceled) el('settingsBasePath').value = result.path;
        } catch (err) {
            logStatus('ERR', `Could not open the folder browser: ${err && err.message ? err.message : err}`);
        }
    });

    el('btnBrowseDriversBasePath').addEventListener('click', async () => {
        try {
            const result = await App.PickFolder(el('settingsDriversBasePath').value);
            if (!result.canceled) el('settingsDriversBasePath').value = result.path;
        } catch (err) {
            logStatus('ERR', `Could not open the folder browser: ${err && err.message ? err.message : err}`);
        }
    });

    el('btnBrowsePreinstallBasePath').addEventListener('click', async () => {
        try {
            // PickPreinstallFolder, not PickFolder - this field resolves its
            // current value against the user's own home directory, not the
            // exe's own location (see preinstallBasePath's own doc comment,
            // app.go), so it needs its own starting-directory resolution.
            const result = await App.PickPreinstallFolder(el('settingsPreinstallBasePath').value);
            if (!result.canceled) el('settingsPreinstallBasePath').value = result.path;
        } catch (err) {
            logStatus('ERR', `Could not open the folder browser: ${err && err.message ? err.message : err}`);
        }
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
        // directDownloadUrls: manufacturer -> platform -> family -> URL,
        // built from each field's own data-mfg/data-platform/data-family
        // attributes (renderSettingsDirectDownloadsPanel) - an empty value
        // is sent through as-is, same as manufacturerUrls above; SaveSettings
        // (Go) fills any blank slot back in from its own defaults rather than
        // leaving it genuinely empty.
        const directDownloadUrls = {};
        for (const input of el('settingsDirectDownloadsPanel').querySelectorAll('.settings-direct-download-url')) {
            const {mfg, platform, family} = input.dataset;
            directDownloadUrls[mfg] ??= {};
            directDownloadUrls[mfg][platform] ??= {};
            directDownloadUrls[mfg][platform][family] = input.value;
        }
        const manufacturerOrder = currentManufacturerOrder();
        const saved = await App.SaveSettings({
            saveFileBasePath: el('settingsBasePath').value,
            driversBasePath: el('settingsDriversBasePath').value,
            preinstallBasePath: el('settingsPreinstallBasePath').value,
            manufacturerUrls,
            directDownloadUrls,
            manufacturerOrder,
            cloudSync: {
                endpoint: el('cloudSyncEndpoint').value,
                bucket: el('cloudSyncBucket').value,
                prefix: el('cloudSyncPrefix').value,
                accessKeyId: el('cloudSyncAccessKeyId').value,
                concurrentTransfers: parseInt(el('cloudSyncConcurrentTransfers').value, 10) || 3,
            },
            // Sent only when non-empty - see cloudSyncSecretKey's own field
            // comment in Settings (Go): empty means "leave the stored
            // secret alone," never "clear it."
            cloudSyncSecretKey: el('cloudSyncSecretKey').value,
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
    // Windows opens the selective Rescan dialog (GitHub issue #10) instead
    // of immediately rescanning - macOS has no .pdt-infcache/"Remove INF"
    // concept to be selective about (its own catalog.<mfg>.json staleness
    // handling already runs automatically on every refresh), so it keeps
    // this exact one-click behavior unchanged.
    el('btnRefreshDrivers').addEventListener('click', async () => {
        if (!isMac()) {
            openRescanModal();
            return;
        }
        const btn = el('btnRefreshDrivers');
        btn.disabled = true;
        // A first-ever/genuinely-changed driver folder scan can still take a
        // while (extracting/inspecting archives - see loadCatalog on either
        // platform; macOS itself is fast on every later refresh once a
        // package's been indexed once - see driver.MacManufacturerCatalog),
        // with nothing else visible changing until it finishes - confirmed
        // live that the disabled button alone reads as "did the click even
        // register?" rather than "working on it". This is the only signal
        // until the OK/ERR line below replaces it.
        logStatus('INFO', 'Refreshing driver catalog...');
        try {
            await applyCatalogStatus(await App.RefreshDriverCatalog(), 'refreshed');
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
