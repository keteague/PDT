export namespace config {
	
	export class SavedRow {
	    Select: boolean;
	    Name: string;
	    IP: string;
	    Manufacturer: string;
	    Model: string;
	    Driver: string;
	    Snmp: boolean;
	    Mono: boolean;
	    OneSided: boolean;
	    UseExistingPort: boolean;
	    AdvancedPrintingFeatures: boolean;
	
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
	        this.Mono = source["Mono"];
	        this.OneSided = source["OneSided"];
	        this.UseExistingPort = source["UseExistingPort"];
	        this.AdvancedPrintingFeatures = source["AdvancedPrintingFeatures"];
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
	export class Settings {
	    saveFileBasePath: string;
	    manufacturerUrls: Record<string, string>;
	
	    static createFrom(source: any = {}) {
	        return new Settings(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.saveFileBasePath = source["saveFileBasePath"];
	        this.manufacturerUrls = source["manufacturerUrls"];
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
	    Mono: boolean;
	    OneSided: boolean;
	    UseExistingPort: boolean;
	    AdvancedPrintingFeatures: boolean;
	
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
	        this.Mono = source["Mono"];
	        this.OneSided = source["OneSided"];
	        this.UseExistingPort = source["UseExistingPort"];
	        this.AdvancedPrintingFeatures = source["AdvancedPrintingFeatures"];
	    }
	}

}

