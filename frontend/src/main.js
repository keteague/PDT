import './style.css';
import './app.css';

import {
    Manufacturers, Models, DriverCandidates, DefaultDriverFor, GetCatalogStatus,
    NewCsvTemplate, ImportCsv, OpenConfiguration, SaveConfiguration, Deploy,
    GetSettings, SaveSettings, PickFolder, OpenManufacturerURL,
    GetAppInfo, OpenRepoURL,
} from '../wailsjs/go/main/App';
import {EventsOn} from '../wailsjs/runtime/runtime';

// Tooltip text, shared between the static template below and the
// dynamically-generated per-row HTML (rowHtml/mfgSelectHtml), so every
// control - defaults, grid headers, and each row's own fields - carries the
// same explanation.
const TIP = {
    salesChainId: 'Included in each deployed printer\'s Comment field as "SalesChain: <id>". Letters, numbers, hyphen, and underscore only.',
    manufacturer: 'Printer manufacturer - determines which drivers are offered.',
    model: 'Optional model filter to narrow the Driver list (Kyocera only; other manufacturers\' driver names aren\'t model-specific).',
    driver: 'Driver to install/use for this printer. Type to fuzzy-search; multiple local versions of the same driver appear as separate dated entries.',
    subnet: 'Pre-fills new rows\' IP with this subnet (a trailing "." is added automatically if you don\'t type one) - e.g. "10.1.1." so you only need to type the last octet per row.',
    portPrefixEnabled: 'When creating a new Standard TCP/IP port, prefix its name with the text to the right instead of using the bare IP address.',
    portPrefixText: 'Prefix text used when Port name prefix is checked, e.g. "IP_" - the port would be named "IP_10.1.1.50".',
    useExistingPort: 'Reuse the Standard TCP/IP port already configured for this row\'s IP instead of creating a new one. Falls back to creating a new port if none already targets the IP.',
    snmp: 'Enable SNMP status monitoring on this printer\'s port (only applies when a new port is created).',
    mono: 'Deploy this printer set to monochrome (black & white) printing by default.',
    oneSided: 'Deploy this printer set to simplex (single-sided) printing by default.',
    apf: 'Enable "Advanced printing features" on the printer\'s Advanced tab.',
    select: 'Included in the next deploy.',
    name: 'Printer object name.',
    ip: 'Printer\'s IP address, or "NUL" to bind permanently to the local NUL: port.',
    selectAllHeader: 'Check/uncheck every row.',
    saveFileBasePath: 'The folder Open/Save Configuration start from by default.',
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
        snmp: false, mono: false, oneSided: false, useExistingPort: false, advancedPrintingFeatures: false,
    }, overrides);
}

function rowToPrinterRow(r) {
    return {
        Name: r.name, IP: r.ip, Manufacturer: r.manufacturer, Model: r.model, Driver: r.driver,
        SNMP: r.snmp, Mono: r.mono, OneSided: r.oneSided,
        UseExistingPort: r.useExistingPort, AdvancedPrintingFeatures: r.advancedPrintingFeatures,
    };
}

function printerRowToRow(pr, select = true) {
    return newRow({
        select, name: pr.Name, ip: pr.IP, manufacturer: pr.Manufacturer, model: pr.Model, driver: pr.Driver,
        snmp: pr.SNMP, mono: pr.Mono, oneSided: pr.OneSided,
        useExistingPort: pr.UseExistingPort, advancedPrintingFeatures: pr.AdvancedPrintingFeatures,
    });
}

function rowToSavedRow(r) {
    return {
        Select: r.select, Name: r.name, IP: r.ip, Manufacturer: r.manufacturer, Model: r.model, Driver: r.driver,
        Snmp: r.snmp, Mono: r.mono, OneSided: r.oneSided,
        UseExistingPort: r.useExistingPort, AdvancedPrintingFeatures: r.advancedPrintingFeatures,
    };
}

function savedRowToRow(sr) {
    return newRow({
        select: sr.Select, name: sr.Name, ip: sr.IP, manufacturer: sr.Manufacturer, model: sr.Model, driver: sr.Driver,
        snmp: sr.Snmp, mono: sr.Mono, oneSided: sr.OneSided,
        useExistingPort: sr.UseExistingPort, advancedPrintingFeatures: sr.AdvancedPrintingFeatures,
    });
}

const state = {
    salesChainId: '',
    portPrefixEnabled: false,
    portPrefixText: '',
    manufacturers: [],
    rows: [],
    deploying: false,
    statusText: '',
    logLines: [],
    settings: {saveFileBasePath: '', manufacturerUrls: {}},
    // Set while Deploy is running: the exact rows submitted, in submission
    // order, plus how many deploy-progress events have arrived so far - since
    // events arrive in that same order, this correlates each event to its
    // row without depending on row Name being unique.
    activeDeploy: null,
};

document.querySelector('#app').innerHTML = `
  <div class="top-bar">
    <label title="${tip('salesChainId')}">SalesChain ID <input type="text" id="salesChainId" size="14" title="${tip('salesChainId')}"></label>
    <button id="btnOpenConfig" title="Load a previously saved JSON configuration (rows + SalesChain ID).">Open Configuration</button>
    <button id="btnSaveConfig" title="Save the current rows and SalesChain ID to a JSON configuration file.">Save Configuration</button>
    <span class="catalog-warning" id="catalogWarning" hidden></span>
    <button id="btnSettings" class="icon-btn" title="Settings">&#9881;</button>
  </div>

  <div class="modal-backdrop" id="settingsBackdrop" hidden>
    <div class="modal">
      <h3>Settings</h3>
      <div class="tabs">
        <button type="button" class="tab-btn active" data-tab="general">General</button>
        <button type="button" class="tab-btn" data-tab="sites">External Sites</button>
        <button type="button" class="tab-btn" data-tab="about">About</button>
      </div>
      <div class="tab-panel" data-tab-panel="general">
        <label class="modal-field" title="${tip('saveFileBasePath')}">
          Save File Base Path
          <div class="path-row">
            <input type="text" id="settingsBasePath" title="${tip('saveFileBasePath')}">
            <button id="btnBrowseBasePath" title="Browse for a folder...">&hellip;</button>
          </div>
        </label>
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
      </div>
      <div class="modal-actions">
        <button id="btnSettingsCancel">Cancel</button>
        <button class="primary" id="btnSettingsSave">Save</button>
      </div>
    </div>
  </div>

  <div class="defaults-panel">
    <fieldset class="defaults-outer">
      <legend>Defaults (used by "Add Printer")</legend>
      <div class="defaults-row">
        <label title="${tip('manufacturer')}">Manufacturer <select id="defMfg" title="${tip('manufacturer')}"></select></label>
        <button type="button" id="btnCheckUpdates" title="Open the selected manufacturer's driver page (configured in Settings &gt; External Sites).">Check for Updates</button>
        <label title="${tip('model')}">Model <div class="combo"><input type="text" id="defModel" title="${tip('model')}"><div class="combo-list" id="defModelList" hidden></div></div></label>
        <label class="driver-label" title="${tip('driver')}">Driver <div class="combo"><input type="text" id="defDriver" title="${tip('driver')}"><div class="combo-list" id="defDriverList" hidden></div></div></label>
      </div>
      <fieldset class="defaults-sub">
        <legend>Port</legend>
        <label title="${tip('subnet')}">Subnet <input type="text" id="defSubnet" placeholder="10.1.1." title="${tip('subnet')}"></label>
        <label title="${tip('portPrefixEnabled')}"><input type="checkbox" id="portPrefixEnabled" title="${tip('portPrefixEnabled')}"> Port name prefix</label>
        <input type="text" id="portPrefixText" size="6" placeholder="IP_" title="${tip('portPrefixText')}">
        <label title="${tip('useExistingPort')}"><input type="checkbox" id="defUseExistingPort" title="${tip('useExistingPort')}"> Use existing port</label>
        <label title="${tip('snmp')}"><input type="checkbox" id="defSnmp" title="${tip('snmp')}"> SNMP</label>
      </fieldset>
      <fieldset class="defaults-sub">
        <legend>Print Defaults</legend>
        <label title="${tip('mono')}"><input type="checkbox" id="defMono" checked title="${tip('mono')}"> Monochrome</label>
        <label title="${tip('oneSided')}"><input type="checkbox" id="defOneSided" checked title="${tip('oneSided')}"> 1-sided</label>
      </fieldset>
      <fieldset class="defaults-sub">
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
    <span class="spacer"></span>
    <span class="status-label" id="statusLabel"></span>
    <button class="primary" id="btnDeploy" title="Deploy every checked row: create/update ports, drivers, and printer objects, then apply print configuration.">Deploy Checked Printers</button>
  </div>

  <div class="grid-wrap">
    <table class="grid">
      <thead>
        <tr>
          <th title="${tip('selectAllHeader')}"><input type="checkbox" id="selectAllHeader" title="${tip('selectAllHeader')}"></th>
          <th title="${tip('name')}">Name</th>
          <th title="${tip('ip')}">IP</th>
          <th title="${tip('manufacturer')}">Manufacturer</th>
          <th title="${tip('model')}">Model</th>
          <th title="${tip('driver')}">Driver</th>
          <th title="${tip('snmp')}">SNMP</th>
          <th title="${tip('mono')}">Mono</th>
          <th title="${tip('oneSided')}">1-sided</th>
          <th title="${tip('useExistingPort')}">UEP</th>
          <th title="Remove this one row, without needing to check it first."></th>
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

// Windows reserved device names (case-insensitive, whole-string only - "PRN1"
// or "COMPANY" are fine, only an exact "PRN"/"COM1"/etc is reserved).
const RESERVED_DEVICE_NAME_RE = /^(CON|PRN|AUX|NUL|COM[0-9]|LPT[0-9])$/i;

// Strips everything except letters, digits, hyphen, and underscore - already
// enough on its own to rule out every DOS/shell-reserved character
// (\/:*?"<>| on Windows, / and : on macOS) without needing to enumerate them.
function sanitizeSalesChainId(raw) {
    return raw.replace(/[^A-Za-z0-9_-]/g, '');
}

// Applies value to both the SalesChain ID field and state, sanitized the
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
// SalesChain ID input handler). Removing the class before re-adding it (with
// a reflow forced in between) restarts the CSS animation even if the
// previous flash from a rapid-fire rejected keystroke hasn't finished yet,
// rather than a no-op re-add the browser would otherwise ignore.
function flashInvalidInput(input) {
    input.classList.remove('input-invalid-flash');
    void input.offsetWidth;
    input.classList.add('input-invalid-flash');
    playInvalidDing();
}

async function init() {
    state.manufacturers = await Manufacturers();
    const mfgSelect = el('defMfg');
    mfgSelect.innerHTML = state.manufacturers.map(m => `<option value="${m}">${m}</option>`).join('');
    mfgSelect.value = state.manufacturers[0] || '';
    if (mfgSelect.value) {
        el('defDriver').value = await DefaultDriverFor(mfgSelect.value);
    }

    const status = await GetCatalogStatus();
    if (!status.ok) {
        const warn = el('catalogWarning');
        warn.hidden = false;
        warn.textContent = `Driver catalog failed to load: ${status.error}`;
    }

    state.settings = await GetSettings();

    renderGrid();
    wireEvents();
    setupDefaultsComboboxes();

    EventsOn('deploy-progress', (result) => onDeployProgress(result));
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

// --- Combobox (Model/Driver) ---
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
// naturally reflects the row/defaults' latest Manufacturer/Model selection
// with no separate invalidation step needed - the row's Model, for instance,
// is only actually looked up at the moment the Driver combobox is opened or
// typed into. onChange(value) is called with every keystroke and on commit.
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

let modelsByManufacturer = {}; // manufacturer -> string[], populated on first use

async function getModelsForManufacturer(manufacturer) {
    if (!modelsByManufacturer[manufacturer]) {
        modelsByManufacturer[manufacturer] = await Models(manufacturer);
    }
    return modelsByManufacturer[manufacturer];
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
}

function rowHtml(r) {
    const cls = r._failed ? 'row-failed' : (r._succeeded ? 'row-succeeded' : '');
    return `
    <tr data-id="${r._id}" class="${cls}">
      <td class="checkbox-cell"><input type="checkbox" class="row-select" ${r.select ? 'checked' : ''} title="${tip('select')}"></td>
      <td><input type="text" class="row-name" value="${attr(r.name)}" title="${tip('name')}"></td>
      <td><input type="text" class="row-ip" value="${attr(r.ip)}" placeholder="or NUL" title="${tip('ip')}"></td>
      <td>${mfgSelectHtml(r)}</td>
      <td><div class="combo"><input type="text" class="row-model" value="${attr(r.model)}" title="${tip('model')}"><div class="combo-list" hidden></div></div></td>
      <td><div class="combo"><input type="text" class="row-driver" value="${attr(r.driver)}" title="${tip('driver')}"><div class="combo-list" hidden></div></div></td>
      <td class="checkbox-cell"><input type="checkbox" class="row-snmp" ${r.snmp ? 'checked' : ''} title="${tip('snmp')}"></td>
      <td class="checkbox-cell"><input type="checkbox" class="row-mono" ${r.mono ? 'checked' : ''} title="${tip('mono')}"></td>
      <td class="checkbox-cell"><input type="checkbox" class="row-onesided" ${r.oneSided ? 'checked' : ''} title="${tip('oneSided')}"></td>
      <td class="checkbox-cell"><input type="checkbox" class="row-uep" ${r.useExistingPort ? 'checked' : ''} title="${tip('useExistingPort')}"></td>
      <td class="checkbox-cell"><button class="row-remove" title="Remove this row">&times;</button></td>
    </tr>`;
}

function mfgSelectHtml(r) {
    const opts = state.manufacturers.map(m => `<option value="${m}" ${m === r.manufacturer ? 'selected' : ''}>${m}</option>`).join('');
    return `<select class="row-mfg" title="${tip('manufacturer')}">${opts}</select>`;
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
        });
        const nameInput = tr.querySelector('.row-name');
        nameInput.addEventListener('input', (e) => { row.name = e.target.value; });
        nameInput.addEventListener('keydown', (e) => { if (e.key === 'Enter') addPrinterRow(true); });

        const ipInput = tr.querySelector('.row-ip');
        ipInput.addEventListener('input', (e) => { row.ip = e.target.value; });
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
        tr.querySelector('.row-snmp').addEventListener('change', (e) => { row.snmp = e.target.checked; });
        tr.querySelector('.row-mono').addEventListener('change', (e) => { row.mono = e.target.checked; });
        tr.querySelector('.row-onesided').addEventListener('change', (e) => { row.oneSided = e.target.checked; });
        tr.querySelector('.row-uep').addEventListener('change', (e) => { row.useExistingPort = e.target.checked; });

        const mfgSelect = tr.querySelector('.row-mfg');
        mfgSelect.addEventListener('change', (e) => {
            row.manufacturer = e.target.value;
            row.model = '';
            row.driver = '';
            renderGrid();
        });

        const modelCombo = tr.querySelector('.row-model').closest('.combo');
        setupCombobox(
            modelCombo.querySelector('input'),
            modelCombo.querySelector('.combo-list'),
            async (filterText) => {
                const all = await getModelsForManufacturer(row.manufacturer);
                const ft = filterText.trim().toLowerCase();
                return ft ? all.filter(m => m.toLowerCase().includes(ft)) : all;
            },
            (value) => { row.model = value; },
            () => addPrinterRow(true),
        );

        const driverCombo = tr.querySelector('.row-driver').closest('.combo');
        setupCombobox(
            driverCombo.querySelector('input'),
            driverCombo.querySelector('.combo-list'),
            (filterText) => DriverCandidates(row.manufacturer, row.model, filterText),
            (value) => { row.driver = value; },
            () => addPrinterRow(true),
        );

        tr.querySelector('.row-remove').addEventListener('click', () => {
            state.rows = state.rows.filter(r => r._id !== row._id);
            renderGrid();
        });
    }
}

function updateSelectAllHeaderState() {
    const header = el('selectAllHeader');
    header.checked = state.rows.length > 0 && state.rows.every(r => r.select);
}

function setupDefaultsComboboxes() {
    setupCombobox(
        el('defModel'),
        el('defModelList'),
        async (filterText) => {
            const all = await getModelsForManufacturer(el('defMfg').value);
            const ft = filterText.trim().toLowerCase();
            return ft ? all.filter(m => m.toLowerCase().includes(ft)) : all;
        },
        () => {},
    );
    setupCombobox(
        el('defDriver'),
        el('defDriverList'),
        (filterText) => DriverCandidates(el('defMfg').value, el('defModel').value, filterText),
        () => {},
    );
}

// --- Toolbar actions ---

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
        model: el('defModel').value,
        driver: el('defDriver').value,
        snmp: el('defSnmp').checked,
        mono: el('defMono').checked,
        oneSided: el('defOneSided').checked,
        useExistingPort: el('defUseExistingPort').checked,
        advancedPrintingFeatures: el('defApf').checked,
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

    el('portPrefixEnabled').addEventListener('change', (e) => {
        state.portPrefixEnabled = e.target.checked;
        el('portPrefixText').disabled = !e.target.checked;
    });
    el('portPrefixText').disabled = true;
    el('portPrefixText').addEventListener('input', (e) => { state.portPrefixText = e.target.value; });

    el('defMfg').addEventListener('change', async () => {
        el('defModel').value = '';
        el('defDriver').value = await DefaultDriverFor(el('defMfg').value);
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
        const result = await NewCsvTemplate();
        if (!result.canceled) setStatus(`Wrote new CSV template to ${result.path}`);
    });

    el('btnImportCsv').addEventListener('click', async () => {
        const result = await ImportCsv();
        if (result.canceled) return;
        state.rows.push(...result.rows.map(pr => printerRowToRow(pr, true)));
        renderGrid();
        setStatus(`Imported ${result.rows.length} row(s) from CSV.`);
    });

    el('btnOpenConfig').addEventListener('click', async () => {
        const result = await OpenConfiguration();
        if (result.canceled) return;
        setSalesChainId(result.config.SalesChainId, {rejectReservedAsEmpty: true});
        state.rows = (result.config.Printers || []).map(savedRowToRow);
        renderGrid();
        setStatus(`Loaded configuration (${state.rows.length} row(s)).`);
    });

    el('btnSaveConfig').addEventListener('click', async () => {
        const cfg = {SalesChainId: state.salesChainId, Printers: state.rows.map(rowToSavedRow)};
        const result = await SaveConfiguration(cfg);
        if (!result.canceled) setStatus(`Saved configuration to ${result.path}`);
    });

    el('btnClearLog').addEventListener('click', () => {
        state.logLines = [];
        renderLog();
    });

    el('btnDeploy').addEventListener('click', deploy);

    wireSettingsModal();
}

// --- Settings modal ---

function renderSettingsSitesPanel() {
    const panel = el('settingsSitesPanel');
    panel.innerHTML = state.manufacturers.map(mfg => `
        <label class="modal-field">
          ${mfg}
          <input type="text" class="settings-url" data-mfg="${attr(mfg)}">
        </label>
    `).join('');
}

function openSettingsModal() {
    el('settingsBasePath').value = state.settings.saveFileBasePath;
    for (const input of el('settingsSitesPanel').querySelectorAll('.settings-url')) {
        input.value = state.settings.manufacturerUrls?.[input.dataset.mfg] || '';
    }
    switchSettingsTab('general');
    el('settingsBackdrop').hidden = false;
}

function closeSettingsModal() {
    el('settingsBackdrop').hidden = true;
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
    const info = await GetAppInfo();
    el('aboutName').textContent = info.name;
    el('aboutVersion').textContent = info.version;
    el('aboutAuthor').textContent = info.author;
    const link = el('aboutRepoLink');
    link.textContent = info.repoUrl;
    link.addEventListener('click', (e) => {
        e.preventDefault();
        OpenRepoURL();
    });
}

function wireSettingsModal() {
    renderSettingsSitesPanel();
    renderAboutPanel();

    el('btnSettings').addEventListener('click', openSettingsModal);
    el('btnSettingsCancel').addEventListener('click', closeSettingsModal);

    for (const btn of document.querySelectorAll('.tab-btn')) {
        btn.addEventListener('click', () => switchSettingsTab(btn.dataset.tab));
    }

    // Clicking the dim backdrop itself (not the modal box) closes it, same
    // as Cancel - but only when the click started and ended on the backdrop,
    // so dragging a text selection from inside the modal out over the
    // backdrop before releasing doesn't accidentally close it.
    let backdropMouseDownOnSelf = false;
    el('settingsBackdrop').addEventListener('mousedown', (e) => {
        backdropMouseDownOnSelf = e.target === e.currentTarget;
    });
    el('settingsBackdrop').addEventListener('click', (e) => {
        if (e.target === e.currentTarget && backdropMouseDownOnSelf) closeSettingsModal();
    });

    el('btnBrowseBasePath').addEventListener('click', async () => {
        const result = await PickFolder(el('settingsBasePath').value);
        if (!result.canceled) el('settingsBasePath').value = result.path;
    });

    el('btnSettingsSave').addEventListener('click', async () => {
        const manufacturerUrls = {};
        for (const input of el('settingsSitesPanel').querySelectorAll('.settings-url')) {
            manufacturerUrls[input.dataset.mfg] = input.value;
        }
        const saved = await SaveSettings({saveFileBasePath: el('settingsBasePath').value, manufacturerUrls});
        state.settings = saved;
        closeSettingsModal();
        setStatus('Settings saved.');
    });

    el('btnCheckUpdates').addEventListener('click', () => {
        OpenManufacturerURL(el('defMfg').value);
    });
}

function setStatus(text) {
    state.statusText = text;
    el('statusLabel').textContent = text;
}

async function deploy() {
    const selected = state.rows.filter(r => r.select);
    if (selected.length === 0) {
        setStatus('No rows checked.');
        return;
    }
    if (state.deploying) return;
    state.deploying = true;
    el('btnDeploy').disabled = true;
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
    setStatus(`Deploying ${selected.length} printer(s)...`);
    appendLog([`===== DEPLOYMENT STARTED: ${selected.length} printer(s) =====`]);

    const portPrefix = state.portPrefixEnabled ? state.portPrefixText : '';
    try {
        await Deploy(selected.map(rowToPrinterRow), state.salesChainId, portPrefix);
    } finally {
        state.deploying = false;
        state.activeDeploy = null;
        el('btnDeploy').disabled = false;
        appendLog(['===== DEPLOYMENT COMPLETE ====='].map(l => `${new Date().toLocaleString()} [OK] ${l}`));
        const failed = selected.filter(r => r._failed).length;
        setStatus(failed > 0 ? `Deployment finished with ${failed} failure(s).` : 'Deployment finished successfully.');
    }
}

init();
