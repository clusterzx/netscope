package http

// builtinSignatures is the embedded, extensible web-app fingerprint database. Each entry
// lists matchers (any match detects the app), an optional CPE vendor/product for the CVE
// processor, an optional strong device-type hint and a confidence.
//
// Device-type hints are only honoured for high-confidence signatures.
var builtinSignatures = []signature{
	{
		name: "Proxmox VE", cpeVendor: "proxmox", cpeProduct: "proxmox_ve", deviceType: "hypervisor", confidence: "high",
		matchers: []matcher{title(`Proxmox Virtual Environment`), body(`pve-manager|PVE\.`), body(`proxmox`)},
		version:  ci(`pve-manager[/ ]([0-9][0-9.\-]+)`), versionField: fieldBody,
	},
	{
		name: "Proxmox Backup Server", cpeVendor: "proxmox", cpeProduct: "proxmox_backup_server", confidence: "high",
		matchers: []matcher{title(`Proxmox Backup Server`), body(`proxmox-backup|PBS\.`)},
	},
	{
		name: "Frigate", confidence: "high",
		matchers: []matcher{title(`Frigate`), body(`frigate`)},
	},
	{
		name: "Coolify", confidence: "high",
		matchers: []matcher{title(`Coolify`), body(`coolify`)},
	},
	{
		name: "Portainer", cpeVendor: "portainer", cpeProduct: "portainer", confidence: "high",
		matchers: []matcher{title(`Portainer`), body(`portainer`)},
	},
	{
		name: "Grafana", cpeVendor: "grafana", cpeProduct: "grafana", confidence: "high",
		matchers: []matcher{title(`Grafana`), body(`grafana-app|grafanaBootData|__grafana`)},
		version:  ci(`"version"\s*:\s*"([0-9][0-9.]+)`), versionField: fieldBody,
	},
	{
		name: "Home Assistant", cpeVendor: "home-assistant", cpeProduct: "home-assistant", deviceType: "smart-home", confidence: "high",
		matchers: []matcher{title(`Home Assistant`), body(`home-assistant|hass|/frontend_latest/`)},
	},
	{
		name: "Synology DSM", cpeVendor: "synology", cpeProduct: "diskstation_manager", deviceType: "nas", confidence: "high",
		matchers: []matcher{title(`Synology|DiskStation`), body(`SYNO\.|synohdpack|dsm-`)},
	},
	{
		name: "OpenWrt LuCI", cpeVendor: "openwrt", cpeProduct: "openwrt", deviceType: "router", confidence: "high",
		matchers: []matcher{header("X-LuCI-Login-Required", `.`), body(`luci-static|cgi-bin/luci`), title(`LuCI`)},
	},
	{
		name: "GL.iNet", deviceType: "router", confidence: "high",
		matchers: []matcher{body(`gl-ui|glinet|gl\.inet`), title(`GL\.iNet`)},
	},
	{
		name: "Jellyfin", cpeVendor: "jellyfin", cpeProduct: "jellyfin", deviceType: "media-player", confidence: "high",
		matchers: []matcher{title(`Jellyfin`), body(`jellyfin`), header("X-Response-Time", `.`)},
	},
	{
		name: "Paperless-ngx", cpeVendor: "paperless-ngx", cpeProduct: "paperless-ngx", confidence: "high",
		matchers: []matcher{title(`Paperless-ngx|Paperless`), body(`paperless`)},
	},
	{
		name: "Pi-hole", cpeVendor: "pi-hole", cpeProduct: "pi-hole", confidence: "high",
		matchers: []matcher{title(`Pi-hole`), body(`pi-hole|pihole`)},
	},
	{
		name: "AdGuard Home", cpeVendor: "adguard", cpeProduct: "adguard_home", confidence: "high",
		matchers: []matcher{title(`AdGuard Home`), body(`AdGuardHome|adguard`)},
	},
	{
		name: "Nextcloud", cpeVendor: "nextcloud", cpeProduct: "nextcloud", confidence: "high",
		matchers: []matcher{title(`Nextcloud`), body(`nextcloud|data-requesttoken`), header("X-Powered-By", `Nextcloud`)},
	},
	{
		name: "Uptime Kuma", confidence: "high",
		matchers: []matcher{title(`Uptime Kuma`), body(`uptime-kuma`)},
	},
	{
		name: "Plex", cpeVendor: "plex", cpeProduct: "plex_media_server", deviceType: "media-player", confidence: "high",
		matchers: []matcher{header("X-Plex-Protocol", `.`), title(`Plex`), body(`Plex Media Server`)},
	},
	{
		name: "Emby", deviceType: "media-player", confidence: "high",
		matchers: []matcher{title(`Emby`), body(`emby|EmbyServer`), header("X-Emby-Version", `.`)},
	},
	{
		name: "Node-RED", cpeVendor: "nodered", cpeProduct: "node-red", confidence: "high",
		matchers: []matcher{title(`Node-RED`), body(`node-red|nodered`)},
	},
	{
		name: "Traefik", cpeVendor: "traefik", cpeProduct: "traefik", confidence: "high",
		matchers: []matcher{title(`Traefik`), body(`traefik`)},
	},
	{
		name: "Nginx Proxy Manager", confidence: "high",
		matchers: []matcher{title(`Nginx Proxy Manager`), body(`nginx-proxy-manager`)},
	},
	{
		name: "Gitea", cpeVendor: "gitea", cpeProduct: "gitea", confidence: "high",
		matchers: []matcher{title(`Gitea`), body(`gitea`), header("Set-Cookie", `i_like_gitea`)},
	},
	{
		name: "Forgejo", cpeVendor: "forgejo", cpeProduct: "forgejo", confidence: "high",
		matchers: []matcher{title(`Forgejo`), body(`forgejo`), header("Set-Cookie", `i_like_forgejo`)},
	},
	{
		name: "Vaultwarden", cpeVendor: "vaultwarden", cpeProduct: "vaultwarden", confidence: "high",
		matchers: []matcher{title(`Vaultwarden|Bitwarden`), body(`vaultwarden`)},
	},
	{
		name: "UniFi Network", cpeVendor: "ui", cpeProduct: "unifi_network_application", confidence: "high",
		matchers: []matcher{title(`UniFi`), body(`unifi`), header("Set-Cookie", `unifises|UNIFI`)},
	},
	{
		name: "TrueNAS", cpeVendor: "truenas", cpeProduct: "truenas", deviceType: "nas", confidence: "high",
		matchers: []matcher{title(`TrueNAS`), body(`truenas`)},
	},
	{
		name: "OPNsense", cpeVendor: "opnsense", cpeProduct: "opnsense", deviceType: "router", confidence: "high",
		matchers: []matcher{title(`OPNsense`), body(`opnsense`)},
	},
	{
		name: "pfSense", cpeVendor: "netgate", cpeProduct: "pfsense", deviceType: "router", confidence: "high",
		matchers: []matcher{title(`pfSense`), body(`pfSense`)},
	},
	{
		name: "NetAlertX", confidence: "high",
		matchers: []matcher{title(`NetAlertX`), body(`netalertx|pialert`)},
	},
	{
		name: "Immich", cpeVendor: "immich", cpeProduct: "immich", confidence: "high",
		matchers: []matcher{title(`Immich`), body(`immich`)},
	},
	{
		name: "Zigbee2MQTT", confidence: "high",
		matchers: []matcher{title(`Zigbee2MQTT`), body(`zigbee2mqtt`)},
	},
	{
		name: "ESPHome", cpeVendor: "esphome", cpeProduct: "esphome", deviceType: "iot", confidence: "high",
		matchers: []matcher{title(`ESPHome`), body(`esphome`)},
	},
	{
		name: "OctoPrint", cpeVendor: "octoprint", cpeProduct: "octoprint", confidence: "high",
		matchers: []matcher{title(`OctoPrint`), body(`octoprint`), header("X-Api-Version", `.`)},
	},
	{
		name: "Sonarr", confidence: "high",
		matchers: []matcher{title(`Sonarr`), body(`sonarr`)},
	},
	{
		name: "Radarr", confidence: "high",
		matchers: []matcher{title(`Radarr`), body(`radarr`)},
	},
	{
		name: "qBittorrent", cpeVendor: "qbittorrent", cpeProduct: "qbittorrent", confidence: "high",
		matchers: []matcher{title(`qBittorrent`), body(`qbittorrent`)},
	},
	{
		name: "MinIO", cpeVendor: "minio", cpeProduct: "minio", confidence: "high",
		matchers: []matcher{server(`MinIO`), title(`MinIO`), body(`minio`)},
	},
	{
		name: "Prometheus", cpeVendor: "prometheus", cpeProduct: "prometheus", confidence: "high",
		matchers: []matcher{title(`Prometheus`), body(`prometheus`)},
	},
	{
		name: "InfluxDB", cpeVendor: "influxdata", cpeProduct: "influxdb", confidence: "high",
		matchers: []matcher{header("X-Influxdb-Version", `.`), title(`InfluxDB`), body(`influxdb`)},
	},
	{
		name: "Cockpit", cpeVendor: "redhat", cpeProduct: "cockpit", confidence: "high",
		matchers: []matcher{title(`Cockpit`), body(`cockpit`)},
	},
	{
		name: "Webmin", cpeVendor: "webmin", cpeProduct: "webmin", confidence: "high",
		matchers: []matcher{title(`Webmin`), body(`webmin`), server(`MiniServ`)},
	},
	{
		name: "AVM FRITZ!Box", cpeVendor: "avm", cpeProduct: "fritz\\!os", deviceType: "router", confidence: "high",
		matchers: []matcher{title(`FRITZ!Box`), body(`fritz|avm`)},
	},
	{
		name: "Tasmota", cpeVendor: "tasmota", cpeProduct: "tasmota", deviceType: "iot", confidence: "high",
		matchers: []matcher{title(`Tasmota`), body(`tasmota`)},
	},
	{
		name: "Shelly", deviceType: "iot", confidence: "high",
		matchers: []matcher{title(`Shelly`), body(`shelly`)},
	},
}
