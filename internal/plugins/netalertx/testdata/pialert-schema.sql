-- Pi.Alert / NetAlertX up to v24.10 schema: Devices table of back/app_old.db plus the columns
-- upgradeDB() added with ALTER TABLE (server/database.py, tag v24.10.12).
CREATE TABLE Devices (dev_MAC STRING (50) PRIMARY KEY NOT NULL COLLATE NOCASE, dev_Name STRING (50) NOT NULL DEFAULT "(unknown)", dev_Owner STRING (30) DEFAULT "(unknown)" NOT NULL, dev_DeviceType STRING (30), dev_Vendor STRING (250), dev_Favorite BOOLEAN CHECK (dev_Favorite IN (0, 1)) DEFAULT (0) NOT NULL, dev_Group STRING (10), dev_Comments TEXT, dev_FirstConnection DATETIME NOT NULL, dev_LastConnection DATETIME NOT NULL, dev_LastIP STRING (50) NOT NULL COLLATE NOCASE, dev_StaticIP BOOLEAN DEFAULT (0) NOT NULL CHECK (dev_StaticIP IN (0, 1)), dev_ScanCycle INTEGER DEFAULT (1) NOT NULL, dev_LogEvents BOOLEAN NOT NULL DEFAULT (1) CHECK (dev_LogEvents IN (0, 1)), dev_AlertEvents BOOLEAN NOT NULL DEFAULT (1) CHECK (dev_AlertEvents IN (0, 1)), dev_AlertDeviceDown BOOLEAN NOT NULL DEFAULT (0) CHECK (dev_AlertDeviceDown IN (0, 1)), dev_SkipRepeated INTEGER DEFAULT 0 NOT NULL, dev_LastNotification DATETIME, dev_PresentLastScan BOOLEAN NOT NULL DEFAULT (0) CHECK (dev_PresentLastScan IN (0, 1)), dev_NewDevice BOOLEAN NOT NULL DEFAULT (1) CHECK (dev_NewDevice IN (0, 1)), dev_Location STRING (250) COLLATE NOCASE, dev_Archived BOOLEAN NOT NULL DEFAULT (0) CHECK (dev_Archived IN (0, 1)));
ALTER TABLE "Devices" ADD "dev_Network_Node_MAC_ADDR" TEXT;
ALTER TABLE "Devices" ADD "dev_Network_Node_port" INTEGER;
ALTER TABLE "Devices" ADD "dev_Icon" TEXT;
ALTER TABLE "Devices" ADD "dev_GUID" TEXT;
ALTER TABLE "Devices" ADD "dev_NetworkSite" TEXT;
ALTER TABLE "Devices" ADD "dev_SSID" TEXT;
ALTER TABLE "Devices" ADD "dev_SyncHubNodeName" TEXT;
