export namespace config {
	
	export class SavedRow {
	    Select: boolean;
	    ID: string;
	    Name: string;
	    IP: string;
	    LPDQueueName: string;
	    Manufacturer: string;
	    Model: string;
	    Driver: string;
	    MacDriver: string;
	    WindowsDisabled: boolean;
	    MacEnabled: boolean;
	    Snmp: boolean;
	    SnmpCommunity: string;
	    Mono: boolean;
	    OneSided: boolean;
	    UseExistingPort: boolean;
	    AdvancedPrintingFeatures: boolean;
	    DevModeFile: string;
	    preDatesMacDriverSplit: boolean;
	
	    static createFrom(source: any = {}) {
	        return new SavedRow(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.Select = source["Select"];
	        this.ID = source["ID"];
	        this.Name = source["Name"];
	        this.IP = source["IP"];
	        this.LPDQueueName = source["LPDQueueName"];
	        this.Manufacturer = source["Manufacturer"];
	        this.Model = source["Model"];
	        this.Driver = source["Driver"];
	        this.MacDriver = source["MacDriver"];
	        this.WindowsDisabled = source["WindowsDisabled"];
	        this.MacEnabled = source["MacEnabled"];
	        this.Snmp = source["Snmp"];
	        this.SnmpCommunity = source["SnmpCommunity"];
	        this.Mono = source["Mono"];
	        this.OneSided = source["OneSided"];
	        this.UseExistingPort = source["UseExistingPort"];
	        this.AdvancedPrintingFeatures = source["AdvancedPrintingFeatures"];
	        this.DevModeFile = source["DevModeFile"];
	        this.preDatesMacDriverSplit = source["preDatesMacDriverSplit"];
	    }
	}
	export class SavedConfig {
	    SalesChainId: string;
	    Printers: SavedRow[];
	
	    static createFrom(source: any = {}) {
	        return new SavedConfig(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.SalesChainId = source["SalesChainId"];
	        this.Printers = this.convertValues(source["Printers"], SavedRow);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}

}

export namespace driver {
	
	export class RescanPackage {
	    name: string;
	    relPath: string;
	
	    static createFrom(source: any = {}) {
	        return new RescanPackage(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.relPath = source["relPath"];
	    }
	}
	export class RescanManufacturer {
	    name: string;
	    packages: RescanPackage[];
	    hasMacCatalogFile: boolean;
	
	    static createFrom(source: any = {}) {
	        return new RescanManufacturer(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.packages = this.convertValues(source["packages"], RescanPackage);
	        this.hasMacCatalogFile = source["hasMacCatalogFile"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}

}

export namespace main {
	
	export class AppInfo {
	    name: string;
	    version: string;
	    author: string;
	    repoUrl: string;
	
	    static createFrom(source: any = {}) {
	        return new AppInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.version = source["version"];
	        this.author = source["author"];
	        this.repoUrl = source["repoUrl"];
	    }
	}
	export class ApplyUpdateResult {
	    error: string;
	
	    static createFrom(source: any = {}) {
	        return new ApplyUpdateResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.error = source["error"];
	    }
	}
	export class FlashNote {
	    level: string;
	    text: string;
	
	    static createFrom(source: any = {}) {
	        return new FlashNote(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.level = source["level"];
	        this.text = source["text"];
	    }
	}
	export class BatchDriveResult {
	    succeeded: string[];
	    failed: Record<string, string>;
	    notes: FlashNote[];
	
	    static createFrom(source: any = {}) {
	        return new BatchDriveResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.succeeded = source["succeeded"];
	        this.failed = source["failed"];
	        this.notes = this.convertValues(source["notes"], FlashNote);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class CatalogStatus {
	    ok: boolean;
	    error: string;
	    hasDrivers: boolean;
	    modelChanges: string[];
	
	    static createFrom(source: any = {}) {
	        return new CatalogStatus(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.ok = source["ok"];
	        this.error = source["error"];
	        this.hasDrivers = source["hasDrivers"];
	        this.modelChanges = source["modelChanges"];
	    }
	}
	export class CloudSyncPlanItem {
	    relPath: string;
	    action: string;
	    localSize: number;
	    remoteSize: number;
	    selected: boolean;
	    isNew: boolean;
	
	    static createFrom(source: any = {}) {
	        return new CloudSyncPlanItem(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.relPath = source["relPath"];
	        this.action = source["action"];
	        this.localSize = source["localSize"];
	        this.remoteSize = source["remoteSize"];
	        this.selected = source["selected"];
	        this.isNew = source["isNew"];
	    }
	}
	export class CloudSyncPlanResult {
	    items: CloudSyncPlanItem[];
	    error: string;
	
	    static createFrom(source: any = {}) {
	        return new CloudSyncPlanResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.items = this.convertValues(source["items"], CloudSyncPlanItem);
	        this.error = source["error"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class CloudSyncResult {
	    succeeded: string[];
	    failed: Record<string, string>;
	
	    static createFrom(source: any = {}) {
	        return new CloudSyncResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.succeeded = source["succeeded"];
	        this.failed = source["failed"];
	    }
	}
	export class CloudSyncSettings {
	    endpoint: string;
	    bucket: string;
	    prefix: string;
	    accessKeyId: string;
	    concurrentTransfers: number;
	
	    static createFrom(source: any = {}) {
	        return new CloudSyncSettings(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.endpoint = source["endpoint"];
	        this.bucket = source["bucket"];
	        this.prefix = source["prefix"];
	        this.accessKeyId = source["accessKeyId"];
	        this.concurrentTransfers = source["concurrentTransfers"];
	    }
	}
	export class DeployRowResult {
	    rowName: string;
	    log: string[];
	    error: string;
	
	    static createFrom(source: any = {}) {
	        return new DeployRowResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.rowName = source["rowName"];
	        this.log = source["log"];
	        this.error = source["error"];
	    }
	}
	export class DevModeResult {
	    fileName: string;
	    canceled: boolean;
	    error: string;
	
	    static createFrom(source: any = {}) {
	        return new DevModeResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.fileName = source["fileName"];
	        this.canceled = source["canceled"];
	        this.error = source["error"];
	    }
	}
	export class DriveInfo {
	    letter: string;
	    label: string;
	    totalBytes: number;
	    freeBytes: number;
	
	    static createFrom(source: any = {}) {
	        return new DriveInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.letter = source["letter"];
	        this.label = source["label"];
	        this.totalBytes = source["totalBytes"];
	        this.freeBytes = source["freeBytes"];
	    }
	}
	export class ExportCollisionResult {
	    sourceFiles: string[];
	    colliding: string[];
	    error: string;
	
	    static createFrom(source: any = {}) {
	        return new ExportCollisionResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.sourceFiles = source["sourceFiles"];
	        this.colliding = source["colliding"];
	        this.error = source["error"];
	    }
	}
	export class ExportResult {
	    destPath: string;
	    copied: string[];
	    error: string;
	
	    static createFrom(source: any = {}) {
	        return new ExportResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.destPath = source["destPath"];
	        this.copied = source["copied"];
	        this.error = source["error"];
	    }
	}
	
	export class ImportResult {
	    canceled: boolean;
	    rows: printer.PrinterRow[];
	
	    static createFrom(source: any = {}) {
	        return new ImportResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.canceled = source["canceled"];
	        this.rows = this.convertValues(source["rows"], printer.PrinterRow);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class ListDrivesResult {
	    drives: DriveInfo[];
	    error: string;
	
	    static createFrom(source: any = {}) {
	        return new ListDrivesResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.drives = this.convertValues(source["drives"], DriveInfo);
	        this.error = source["error"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class LocalPrinterCandidate {
	    name: string;
	    ip: string;
	    manufacturer: string;
	    driver: string;
	    physical: boolean;
	
	    static createFrom(source: any = {}) {
	        return new LocalPrinterCandidate(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.ip = source["ip"];
	        this.manufacturer = source["manufacturer"];
	        this.driver = source["driver"];
	        this.physical = source["physical"];
	    }
	}
	export class MacDriverCandidate {
	    label: string;
	    source: string;
	
	    static createFrom(source: any = {}) {
	        return new MacDriverCandidate(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.label = source["label"];
	        this.source = source["source"];
	    }
	}
	export class ModelCandidate {
	    label: string;
	    source: string;
	
	    static createFrom(source: any = {}) {
	        return new ModelCandidate(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.label = source["label"];
	        this.source = source["source"];
	    }
	}
	export class OpenConfigResult {
	    canceled: boolean;
	    config: config.SavedConfig;
	
	    static createFrom(source: any = {}) {
	        return new OpenConfigResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.canceled = source["canceled"];
	        this.config = this.convertValues(source["config"], config.SavedConfig);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class OpenFolderResult {
	    error: string;
	
	    static createFrom(source: any = {}) {
	        return new OpenFolderResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.error = source["error"];
	    }
	}
	export class OpenPrintingSyncResult {
	    downloaded: number;
	    skipped: number;
	    failed: number;
	    errors: string[];
	    canceled: boolean;
	    error: string;
	
	    static createFrom(source: any = {}) {
	        return new OpenPrintingSyncResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.downloaded = source["downloaded"];
	        this.skipped = source["skipped"];
	        this.failed = source["failed"];
	        this.errors = source["errors"];
	        this.canceled = source["canceled"];
	        this.error = source["error"];
	    }
	}
	export class PathResult {
	    canceled: boolean;
	    path: string;
	
	    static createFrom(source: any = {}) {
	        return new PathResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.canceled = source["canceled"];
	        this.path = source["path"];
	    }
	}
	export class PreinstallFoldersResult {
	    folders: string[];
	    error: string;
	
	    static createFrom(source: any = {}) {
	        return new PreinstallFoldersResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.folders = source["folders"];
	        this.error = source["error"];
	    }
	}
	export class RowDriverProblems {
	    windows: string;
	    mac: string;
	
	    static createFrom(source: any = {}) {
	        return new RowDriverProblems(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.windows = source["windows"];
	        this.mac = source["mac"];
	    }
	}
	export class RunbookPrinter {
	    id: string;
	    name: string;
	    ip: string;
	    lpdQueueName: string;
	    useExistingPort: boolean;
	    manufacturer: string;
	    model: string;
	    driver: string;
	    macDriver: string;
	    windowsEnabled: boolean;
	    macEnabled: boolean;
	
	    static createFrom(source: any = {}) {
	        return new RunbookPrinter(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	        this.ip = source["ip"];
	        this.lpdQueueName = source["lpdQueueName"];
	        this.useExistingPort = source["useExistingPort"];
	        this.manufacturer = source["manufacturer"];
	        this.model = source["model"];
	        this.driver = source["driver"];
	        this.macDriver = source["macDriver"];
	        this.windowsEnabled = source["windowsEnabled"];
	        this.macEnabled = source["macEnabled"];
	    }
	}
	export class RunbookResult {
	    path: string;
	    error: string;
	
	    static createFrom(source: any = {}) {
	        return new RunbookResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.path = source["path"];
	        this.error = source["error"];
	    }
	}
	export class Settings {
	    saveFileBasePath: string;
	    driversBasePath: string;
	    preinstallBasePath: string;
	    manufacturerUrls: Record<string, string>;
	    manufacturerOrder: string[];
	    directDownloadUrls: Record<string, any>;
	    cloudSync: CloudSyncSettings;
	    cloudSyncSecretKey?: string;
	    cloudSyncHasSecret: boolean;
	    verboseLoggingDisabled: boolean;
	    logLevel: string;
	
	    static createFrom(source: any = {}) {
	        return new Settings(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.saveFileBasePath = source["saveFileBasePath"];
	        this.driversBasePath = source["driversBasePath"];
	        this.preinstallBasePath = source["preinstallBasePath"];
	        this.manufacturerUrls = source["manufacturerUrls"];
	        this.manufacturerOrder = source["manufacturerOrder"];
	        this.directDownloadUrls = source["directDownloadUrls"];
	        this.cloudSync = this.convertValues(source["cloudSync"], CloudSyncSettings);
	        this.cloudSyncSecretKey = source["cloudSyncSecretKey"];
	        this.cloudSyncHasSecret = source["cloudSyncHasSecret"];
	        this.verboseLoggingDisabled = source["verboseLoggingDisabled"];
	        this.logLevel = source["logLevel"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class SpoolerResult {
	    state: string;
	    error: string;
	
	    static createFrom(source: any = {}) {
	        return new SpoolerResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.state = source["state"];
	        this.error = source["error"];
	    }
	}
	export class UpdateCheckResult {
	    available: boolean;
	    currentVersion: string;
	    latestVersion: string;
	    releaseUrl: string;
	    assetUrl: string;
	    error: string;
	
	    static createFrom(source: any = {}) {
	        return new UpdateCheckResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.available = source["available"];
	        this.currentVersion = source["currentVersion"];
	        this.latestVersion = source["latestVersion"];
	        this.releaseUrl = source["releaseUrl"];
	        this.assetUrl = source["assetUrl"];
	        this.error = source["error"];
	    }
	}
	export class WindowsDriverCandidate {
	    label: string;
	    source: string;
	
	    static createFrom(source: any = {}) {
	        return new WindowsDriverCandidate(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.label = source["label"];
	        this.source = source["source"];
	    }
	}

}

export namespace printer {
	
	export class PrinterRow {
	    Name: string;
	    IP: string;
	    LPDQueueName: string;
	    Manufacturer: string;
	    Model: string;
	    Driver: string;
	    MacDriver: string;
	    WindowsDisabled: boolean;
	    MacEnabled: boolean;
	    SNMP: boolean;
	    SNMPCommunity: string;
	    Mono: boolean;
	    OneSided: boolean;
	    UseExistingPort: boolean;
	    AdvancedPrintingFeatures: boolean;
	    DevModeFile: string;
	
	    static createFrom(source: any = {}) {
	        return new PrinterRow(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.Name = source["Name"];
	        this.IP = source["IP"];
	        this.LPDQueueName = source["LPDQueueName"];
	        this.Manufacturer = source["Manufacturer"];
	        this.Model = source["Model"];
	        this.Driver = source["Driver"];
	        this.MacDriver = source["MacDriver"];
	        this.WindowsDisabled = source["WindowsDisabled"];
	        this.MacEnabled = source["MacEnabled"];
	        this.SNMP = source["SNMP"];
	        this.SNMPCommunity = source["SNMPCommunity"];
	        this.Mono = source["Mono"];
	        this.OneSided = source["OneSided"];
	        this.UseExistingPort = source["UseExistingPort"];
	        this.AdvancedPrintingFeatures = source["AdvancedPrintingFeatures"];
	        this.DevModeFile = source["DevModeFile"];
	    }
	}

}

