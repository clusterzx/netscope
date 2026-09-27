-- Language of the web interface per user: '' follows the browser (Accept-Language), 'de' or
-- 'en' is the user's own choice. It also decides the language of the texts the API returns
-- (plugin settings, event catalog, error messages).
ALTER TABLE users ADD COLUMN locale TEXT NOT NULL DEFAULT '';
