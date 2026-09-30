// English catalog: German source text → English. One file per area of the UI; a text used
// in several areas lives in common.json. scripts/i18n-check.mjs rejects keys that appear in
// more than one file, unused keys and placeholder mismatches.
import auth from './auth.json';
import common from './common.json';
import dashboard from './dashboard.json';
import device from './device.json';
import devices from './devices.json';
import layout from './layout.json';
import monitoring from './monitoring.json';
import plugins from './plugins.json';
import rules from './rules.json';
import system from './system.json';
import topology from './topology.json';
import ui from './ui.json';
import users from './users.json';
import welcome from './welcome.json';

const en = {
	...common,
	...layout,
	...ui,
	...auth,
	...users,
	...dashboard,
	...devices,
	...device,
	...monitoring,
	...topology,
	...plugins,
	...rules,
	...system,
	...welcome
};

export default en;
