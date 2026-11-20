-- hikyo:table orgs class=org chain=-
CREATE TABLE orgs (id TEXT PRIMARY KEY);

-- hikyo:table widgets class=project chain=org_id,project_id
CREATE TABLE widgets (id TEXT PRIMARY KEY, org_id TEXT NOT NULL, project_id TEXT NOT NULL);

CREATE TABLE unclassified (id TEXT PRIMARY KEY);
