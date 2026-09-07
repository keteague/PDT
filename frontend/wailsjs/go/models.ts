export namespace config {
	
	export class SavedRow {
	    Select: boolean;
	    Name: string;
	    IP: string;
	    Manufacturer: string;
	    Model: string;
	    Driver: string;
	    Snmp: boolean;
	    SnmpCommunity: string;
	    Mono: boolean;
	    OneSided: boolean;
	    UseExistingPort: boolean;
	    AdvancedPrintingFeatures: boolean;
	    DevModeFile: string;
	
	    static createFrom(source: any = {}) {
	        return new SavedRow(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.Select = source["Select"];
	        this.Name = source["Name"];
	        this.IP = source["IP"];
	        this.Manufacturer = source["Manufacturer"];
	        this.Model = source["Model"];
	        this.Driver = source["Driver"];
	        this.Snmp = source["Snmp"];
	        this.SnmpCommunity = source["SnmpCommunity"];
	        this.Mono = source["Mono"];
	        this.OneSided = source["OneSided"];
	        this.UseExistingPort = source["UseExistingPort"];
	        this.AdvancedPrintingFeatures = source["AdvancedPrintingFeatures"];
	        this.DevModeFile = source["DevModeFile"];
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
	export class BatchDriveResult {
	    succeeded: string[];
	    failed: Record<string, string>;
	
	    static createFrom(source: any = {}) {
	        return new BatchDriveResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.succeeded = source["succeeded"];
	        this.failed = source["failed"];
	    }
	}
	export class CatalogStatus {
	    ok: boolean;
	    error: string;
	
	    static createFrom(source: any = {}) {
	        return new CatalogStatus(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.ok = source["ok"];
	        this.error = source["error"];
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
	export class Settings {
	    saveFileBasePath: string;
	    driversBasePath: string;
	    preinstallBasePath: string;
	    manufacturerUrls: Record<string, string>;
	    manufacturerOrder: string[];
	
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

}

export namespace printer {
	
	export class PrinterRow {
	    Name: string;
	    IP: string;
	    Manufacturer: string;
	    Model: string;
	    Driver: string;
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
	        this.Manufacturer = source["Manufacturer"];
	        this.Model = source["Model"];
	        this.Driver = source["Driver"];
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

