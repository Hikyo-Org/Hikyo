# Backup snapshot consistency

Base b3fb04eb87d4a3598e60c071563e47a6ff126264, stacked on #655. SQLite schema version now comes from its owned read-only VACUUM snapshot; PostgreSQL version comes from the same serializable read-only deferrable transaction as COPY. No new archive format or upgrade admission introduced.

Both actual-engine forced-race tests fail against the original source (canonical /private/tmp overlay), pass after the fix, and pass three race runs. Full store/app/service/lint with PostgreSQL enabled passed. Independent Standards/Spec review CLEAN plus actual both-engine race replay. HTML decisions: docs/reports/1.0/backup-snapshot.html. Parent owns signed commit, exact-head CI and merge.
