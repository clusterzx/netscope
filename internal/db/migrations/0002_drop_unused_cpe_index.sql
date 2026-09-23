-- The CVE matcher always queries nvd_cpe_matches by (vendor, product); the product-only
-- index is never used and costs ~64 MB with a full NVD mirror.
DROP INDEX IF EXISTS nvd_cpe_matches_product;
