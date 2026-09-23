-- Where a credential applies (JSON plugin.Scope: everywhere, subnets, devices, groups,
-- tags, filter). Plugins pick the most specific applicable credential per target.
ALTER TABLE credentials ADD COLUMN scope TEXT NOT NULL DEFAULT '{"allSubnets":true}';
