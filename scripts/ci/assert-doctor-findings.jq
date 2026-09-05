# Authenticated instance doctor acceptance for a fresh deployment. These are
# measured server findings, not a replacement health response. $engine names
# the deployment under test so remote PostgreSQL capacity cannot pass as local.
def expected_codes:
  ["adapter-targets", "argon2-floor", "backup-rpo", "data-volume",
   "database-durability", "pin-expiry", "project-storage", "reencrypt",
   "restore-drill", "retention-prune", "root-escrow", "root-rotation"] | sort;

(.status == (if $volume_severity == "error" then "error" else "warning" end)) and
([.findings[].code] | sort == expected_codes) and
all(.findings[];
  (.provider == "-") and
  (.message | type == "string" and length > 0) and
  (if .code == "retention-prune" then (.severity == "ok" or .severity == "warn")
   elif .code == "backup-rpo" or .code == "restore-drill" then .severity == "warn"
   elif .code == "root-escrow" then .severity == "warn"
   elif .code == "data-volume" then
     if $engine == "postgres" then .severity == "unknown" and $volume_severity == "unknown"
     elif $engine == "sqlite" then
       (.severity == $volume_severity) and
       (["ok", "warn", "error"] | index($volume_severity) != null) and
       (if .severity == "error" then
         (.message | capture("volume (?<used>[0-9]+[.][0-9]+)%").used | tonumber >= 90)
        else true end)
     else false end
   else .severity == "ok" end))
