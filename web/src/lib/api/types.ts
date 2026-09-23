// Friendly names for the generated API types (see generated.ts) plus a few hand-written
// types for payloads the OpenAPI spec cannot describe (SSE messages, planned endpoints).
import type * as G from './generated';

// ---------------------------------------------------------------- enums (string unions)

export type Severity = 'info' | 'low' | 'medium' | 'high' | 'critical';
export const SEVERITIES: Severity[] = ['info', 'low', 'medium', 'high', 'critical'];
export type DeviceState = 'known' | 'unknown' | 'ignored';
export type Criticality = 'low' | 'normal' | 'high' | 'critical';
export type PluginKind = 'scanner' | 'importer' | 'processor' | 'publisher';
export type RunStatus = 'queued' | 'running' | 'success' | 'failed' | 'timeout' | 'cancelled';
export type HealthState = 'up' | 'down' | 'degraded' | 'unknown';

// ---------------------------------------------------------------- auth & system

export type User = G.AuthUser;
export type Principal = G.AuthPrincipal;
export type Me = G.ApiMeResponse;
export type ApiToken = G.AuthToken;
export type TokenCreated = G.ApiTokenCreated;
export type Meta = G.ApiMetaResponse;
export type DeviceAction = G.ApiDeviceAction;
export type PluginShort = G.ApiPluginShort;
export type SeverityInfo = G.ApiSeverityInfo;
export type SystemInfo = G.ApiSystemInfo;
export type SystemSettings = G.SettingsSystem;
export type BackupInfo = G.ApiBackupInfo;
export type AuditEntry = G.AuditEntry;
export type LogEntry = G.LoggingEntry;
export type CronDescription = G.ApiCronResponse;
export type UploadResponse = G.ApiUploadResponse;
export type OkResponse = G.ApiOkResponse;
export type IdResponse = G.ApiIdResponse;
export type HealthResponse = G.ApiHealthResponse;
export type Dashboard = G.ApiDashboard;
export type DashboardPluginStatus = G.ApiPluginStatus;
export type TopCVE = G.ApiTopCVE;

// ---------------------------------------------------------------- inventory

export type DeviceRow = G.InventoryDeviceRow;
export type DeviceList = G.ApiDeviceList;
export type DeviceDetail = G.InventoryDeviceDetail;
export type DeviceUpdate = G.InventoryDeviceUpdate;
export type GroupRef = G.InventoryGroupRef;
export type Group = G.InventoryGroup;
export type Subnet = G.ApiSubnetView;
export type SubnetInput = G.InventorySubnet;
export type SubnetAccess = 'direct' | 'routed' | 'wireguard';
export type TunnelStatus = G.TunnelStatus;
export type TunnelOverview = G.ApiTunnelOverview;
export type TunnelSummary = G.WgconfSummary;
export type TunnelTestResult = G.TunnelTestResult;
export type CustomField = G.InventoryCustomField;
export type SavedView = G.InventorySavedView;
export type TagCount = G.InventoryTagCount;
export type QueryField = G.InventoryQueryField;
export type FactView = G.InventoryFactView;
export type IPView = G.InventoryIPView;
export type MACView = G.InventoryMACView;
export type PresenceView = G.InventoryPresenceView;
export type RefView = G.InventoryRefView;
export type PortView = G.InventoryPortView;
export type HTTPView = G.InventoryHTTPView;
export type CertView = G.InventoryCertView;
export type PackageView = G.InventoryPackageView;
export type PackageList = G.InventoryPackageList;
export type ContainerData = G.InventoryContainerData;
export type ContainerView = G.InventoryContainerView;
export type ImageView = G.InventoryImageView;
export type ObservationView = G.InventoryObservationView;
export type TimelineEntry = G.InventoryTimelineEntry;
export type Relation = G.InventoryRelation;
export type Graph = G.InventoryGraph;
export type GraphNode = G.InventoryGraphNode;
export type GraphEdge = G.InventoryGraphEdge;
export type DiffResult = G.InventoryDiffResult;
export type DiffItem = G.InventoryDiffItem;
export type DiffSide = G.InventoryDiffSide;
export type BulkRequest = G.ApiBulkRequest;
export type BulkResponse = G.ApiBulkResponse;
export type ScanResponse = G.ApiScanResponse;
export type ActionOutcome = G.PluginhostActionOutcome;

// ---------------------------------------------------------------- time series

export type Series = G.TimeseriesSeries;
export type SeriesPoint = G.TimeseriesPoint;
export type SeriesResponse = G.ApiSeriesResponse;

// ---------------------------------------------------------------- events & diff

export type Event = G.EventsEvent;
export type EventList = G.ApiEventList;
export type EventDetail = G.ApiEventDetail;
export type EventSpec = G.PluginEventSpec;
export type AckRequest = G.ApiAckRequest;

// ---------------------------------------------------------------- plugins & runs

export type PluginView = G.PluginhostPluginView;
export type PluginInfo = G.PluginInfo;
export type PluginConfigView = G.PluginhostConfigView;
export type PluginConfigInput = G.PluginhostConfigInput;
export type PluginAction = G.PluginAction;
export type PluginScope = G.PluginScope;
export type RunView = G.PluginhostRunView;
export type RunList = G.ApiRunList;
export type RunLog = G.PluginhostRunLog;
export type PublisherInfo = G.PluginhostPublisherInfo;

// ---------------------------------------------------------------- settings schema (SchemaForm)

export type SchemaField = G.PluginField;
export type Schema = G.PluginSchema;
export type SchemaOption = G.PluginOption;
export type SchemaCondition = G.PluginCondition;
export type SchemaValidation = G.PluginValidation;
export type FieldType =
	| 'string'
	| 'secret'
	| 'int'
	| 'bool'
	| 'cron'
	| 'enum'
	| 'string-list'
	| 'subnet-list'
	| 'credential-ref'
	| 'duration';
/** Value the API returns instead of a stored secret; sending it back keeps the secret. */
export const SECRET_MASK = '********';

// ---------------------------------------------------------------- rules, credentials, health, reports

export type Rule = G.RulesRule;
export type RuleConditions = G.RulesConditions;
export type RuleAction = G.RulesAction;
export type RuleSimInput = G.RulesSimInput;
export type RuleSimResult = G.RulesSimResult;
export type NotificationView = G.RulesNotificationView;
export type Credential = G.ApiCredentialView;
export type CredentialInput = G.VaultCredentialInput;
export type CredentialType = G.PluginCredentialType;
export type HealthCheck = G.HealthcheckCheck;
export type HealthCheckConfig = G.HealthcheckCheckConfig;
export type HealthBoard = G.ApiHealthBoard;
export type Outage = G.HealthcheckOutage;
export type CheckRunResult = G.ApiCheckRunResult;
export type ChangeReport = G.ReportsChangeReport;

// ---------------------------------------------------------------- planned: device CVEs
// GET /api/v1/devices/{id}/cves and POST /api/v1/vulnerabilities/ignore are not in the
// OpenAPI spec yet; call them with api.raw and these shapes.

export interface DeviceCVE {
	cve: string;
	cvss: number;
	severity: Severity;
	vector?: string;
	description?: string;
	refs?: string[];
	product?: string;
	version?: string;
	cpe?: string;
	source?: string;
	/** exact | range | heuristic */
	matchType?: string;
	firstSeen?: string;
	ignored?: boolean;
	note?: string;
}

export interface CVEIgnoreRequest {
	deviceId: number;
	cve: string;
	ignored: boolean;
	note?: string;
}

// ---------------------------------------------------------------- SSE (/api/v1/stream)

export type LiveTopic =
	'run' | 'run.log' | 'device' | 'event' | 'plugin' | 'notification' | 'health' | 'system' | 'log';

/** One message of the live stream (event name = topic). */
export interface LiveMessage<T = unknown> {
	id: number;
	topic: LiveTopic;
	type: string;
	at: string;
	data: T;
}

/** data of topic "run" (types queued | started | progress | finished). */
export interface RunMessageData {
	id: number;
	pluginId?: string;
	trigger?: string;
	status?: string;
	error?: string;
	durationMs?: number;
	done?: number;
	total?: number;
	/** current RunView (queued, started, finished) */
	run?: RunView;
	/** finished: routine scheduled run without changes – not kept in the history (GET /runs/{id} → 404) */
	discarded?: boolean;
}

/** data of topic "run.log" (type line). */
export interface RunLogMessageData {
	runId: number;
	pluginId: string;
	ts: string;
	level: string;
	msg: string;
	attrs?: Record<string, unknown>;
}

/** data of topic "device" (created | updated | deleted | online | offline). */
export interface DeviceMessageData {
	id: number;
	/** type deleted: the device was merged into this device */
	mergedInto?: number;
}

/** data of topic "event": type created → Event, type acked → {ids} or {count}. */
export type EventMessageData = Event | { ids?: number[]; count?: number };

/** data of topic "plugin" (type updated). */
export interface PluginMessageData {
	id: string;
}

/** data of topic "notification" (sent | failed). */
export interface NotificationMessageData {
	id: number;
	publisher: string;
	error?: string;
}
