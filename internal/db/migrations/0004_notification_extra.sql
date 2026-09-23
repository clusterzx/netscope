-- Rich content of a notification for publishers that support it (reports: an HTML body
-- for mails and the PDF as attachment), JSON plugin.NotificationExtra.
ALTER TABLE notifications ADD COLUMN extra TEXT NOT NULL DEFAULT '{}';
