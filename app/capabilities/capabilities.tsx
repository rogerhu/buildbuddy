declare var window: any;

export class Capabilities {
  name: string;
  version: string;
  paths: Set<string>;
  enterprise: boolean;
  auth: string;

  register(name: string, enterprise: boolean, paths: Array<string>) {
    this.name = name;
    this.version = window.buildbuddyConfig && window.buildbuddyConfig.version;
    this.auth = window.buildbuddyConfig && window.buildbuddyConfig.configured_issuers && window.buildbuddyConfig.configured_issuers.length && window.buildbuddyConfig.configured_issuers[0];
    this.enterprise = enterprise;
    this.paths = new Set(paths);
    window.gtag('set', {
      'app_name': this.name,
      'app_version': this.version,
      'app_installer_id': window.location.host
    });
    window.gtag('config', 'UA-156160991-2');
    this.didNavigateToPath();
  }

  canNavigateToPath(path: string) {
    return this.paths.has(path);
  }

  didNavigateToPath() {
    window.gtag('event', 'path', {
      event_category: 'navigate',
      event_label: window.location.pathname + window.location.search + window.location.hash
    });
  }
}

export default new Capabilities();
