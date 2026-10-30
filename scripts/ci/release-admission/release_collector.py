"""Finite injected GitHub reads and whole-byte integrity replay, never authority.

No default network transport, clock, source adoption or historical reinterpretation.
"""
from __future__ import annotations

import base64
import copy
from datetime import datetime, timedelta
import hashlib
import re

import release_policy

REPO = 'repos/Hikyo-Org/Hikyo'
MODE = release_policy.EVIDENCE_MODE
RELEASE_SCHEMA = 'hikyo-158/github-release-collection/v1'
PAIR_SCHEMA = 'hikyo-158/github-pair-collection/v1'
GRAPHQL = '''query ReleaseMergedEvents($number:Int!,$cursor:String){repository(owner:"Hikyo-Org",name:"Hikyo"){databaseId nameWithOwner owner{login ... on Organization{databaseId}} pullRequest(number:$number){id number state merged mergedAt headRefOid baseRefOid mergeCommit{oid} timelineItems(first:100,after:$cursor,itemTypes:[MERGED_EVENT]){nodes{... on MergedEvent{id createdAt actor{login ... on User{databaseId}} commit{oid}}} pageInfo{hasNextPage endCursor}}}}}'''
FLAGS = {'runtimeReady': False, 'externalAuthenticityVerifiedByCollector': False,
         'executionAuthorityGranted': False}
# These variants use native issue-event identity/time fields. The queue shapes
# are also retained in the pinned historical originals, without causal inference.
# Timeline-only commit/review/comment variants are validated separately below.
TIMELINE_ISSUE_EVENTS = frozenset({
    'added_to_merge_queue', 'removed_from_merge_queue', 'labeled', 'unlabeled',
    'mentioned', 'review_dismissed', 'renamed', 'merged', 'closed',
})


def _constants():
    """The policy module is the sole owner of fixed inventories and bounds."""
    return {'repository': dict(release_policy.REPOSITORY),
            'ownerActor': dict(release_policy.OWNER_ACTOR),
            'trustedPaths': sorted(release_policy.TRUSTED_FILES),
            'candidateCIPaths': sorted(release_policy.CANDIDATE_CI_FILES),
            'requiredCIJobs': {k: dict(v) for k, v in release_policy.REQUIRED_CI_JOBS.items()}}


def _same(left, right):
    """Compare large envelopes without increasing the original JSON decoder cap."""
    if type(left) is not type(right):
        return False
    if type(left) is dict:
        return left.keys() == right.keys() and all(_same(left[k], right[k]) for k in left)
    if type(left) is list:
        return len(left) == len(right) and all(_same(a, b) for a, b in zip(left, right))
    return left == right


def _sha(body):
    return hashlib.sha256(body).hexdigest()


def _pins(files):
    return {path: _sha(body.encode('utf-8')) for path, body in files.items()}


def _positive(m, value):
    m.require(type(value) is int and 0 < value < 10**25, 'positive-exact-provider-id')


def _selection(m, value, kind):
    fields = ({'prNumber', 'mergeSHA', 'runId', 'attempt', 'historicalAnchorSHA'}
              if kind == 'release' else
              {'candidatePRNumber', 'candidateHeadSHA', 'candidateRunId', 'candidateAttempt',
               'mainSHA', 'mainPRNumber', 'mainRunId', 'mainAttempt'})
    m.exact(value, fields)
    for key in fields:
        if key.endswith('SHA'):
            m.require(m.sha(value[key], 40), 'explicit-sha-selection')
        else:
            _positive(m, value[key])


class ExternalOriginals:
    """Existing Root custody must authenticate whole originals outside this module."""

    def verify(self, collection):
        raise ValueError('collector:missing-external-original-authentication')


def require_external_originals(collection, verifier):
    """No caller-supplied boolean, digest, or pure checker supplies this decision."""
    if not isinstance(verifier, ExternalOriginals):
        raise ValueError('collector:external-original-verifier-required')
    if verifier.verify(copy.deepcopy(collection)) is not True:
        raise ValueError('collector:external-originals-refused')
    # No runtime/authenticity flag is changed even on an external decision.


class Collector:
    def __init__(self, m, transport, explicit_clock):
        m.require(callable(transport) and callable(explicit_clock), 'injected-transport-and-clock')
        self.m, self.transport, self.now = m, transport, explicit_clock
        self.p = _constants()
        self.used = False
        self.originals, self.projections, self.cache = [], [], {}
        self.readbacks = []
        self.total_bytes = 0

    def _time(self):
        now = self.now()
        self.m.clock(now)
        if hasattr(self, 'started'):
            self.m.require(self.last <= now and now - self.started <= timedelta(seconds=release_policy.COLLECTION_SECONDS),
                           'monotonic-collection-within-five-minutes')
        self.last = now
        return now

    def _start(self):
        self.m.require(not self.used, 'single-collection-instance')
        self.used = True
        self.started = self._time()

    def _read(self, endpoint, *, body=None, cache=True):
        m = self.m
        pattern = (re.escape(REPO) + r'(?:|/git/ref/heads/main|/pulls/[1-9][0-9]*'
                   r'(?:/(?:commits|timeline)\?per_page=100&page=[1-3])?'
                   r'|/commits/[0-9a-f]{40}|/git/(?:trees|blobs)/[0-9a-f]{40}'
                   r'|/actions/runs/[1-9][0-9]*(?:/attempts/[1-9][0-9]*'
                   r'(?:/jobs\?per_page=100&page=[1-3])?)?)')
        if endpoint == 'graphql':
            m.exact(body, {'query', 'variables'})
            m.exact(body['variables'], {'number', 'cursor'})
            _positive(m, body['variables']['number'])
            cursor = body['variables']['cursor']
            m.require(body['query'] == GRAPHQL and (cursor is None or
                      (type(cursor) is str and 1 <= len(cursor) <= 1024)), 'fixed-read-only-graphql')
        else:
            m.require(type(endpoint) is str and re.fullmatch(pattern, endpoint) is not None
                      and body is None, 'closed-read-endpoint')
        request = {'method': 'POST' if endpoint == 'graphql' else 'GET',
                   'endpoint': endpoint, 'accept': 'application/vnd.github+json', 'body': body,
                   'deadlineAt': m.stamp(self.started + timedelta(seconds=release_policy.COLLECTION_SECONDS))}
        key = m.canonical(request)
        if cache and key in self.cache:
            return self.cache[key]
        m.require(len(self.originals) < release_policy.COLLECTION_REQUESTS, 'finite-request-budget')
        sent = self._time()
        m.require(sent < self.started + timedelta(seconds=release_policy.COLLECTION_SECONDS), 'remaining-read-deadline')
        response = self.transport(copy.deepcopy(request))
        received = self._time()
        m.exact(response, {'status', 'headers', 'body'})
        headers, raw = response['headers'], response['body']
        m.require(type(response['status']) is int and response['status'] == 200
                  and type(headers) is dict and set(headers) <= {
                      'content-type', 'link', 'date', 'etag', 'x-github-request-id',
                      'x-github-api-version-selected'}
                  and all(type(v) is str and len(v) <= 8192 for v in headers.values())
                  and type(raw) is bytes, 'bounded-public-response')
        m.require(headers.get('content-type', '').split(';')[0] in
                  ('application/json', 'application/vnd.github+json'), 'public-json-response')
        self.total_bytes += len(raw)
        m.require(self.total_bytes <= release_policy.COLLECTION_RAW_BYTES, 'whole-response-byte-budget')
        value = m.strict_json(raw)
        index = len(self.originals)
        self.originals.append({'captureIndex': index, 'request': copy.deepcopy(request),
                               'sentAt': m.stamp(sent), 'receivedAt': m.stamp(received),
                               'status': response['status'], 'headers': copy.deepcopy(headers),
                               'body': raw.decode('utf-8'), 'bodyBytes': len(raw),
                               'bodySHA256': _sha(raw), 'semanticSHA256': _sha(m.canonical(value))})
        result = value, index, headers
        self.cache[key] = result
        return result

    def _map(self, destination, index, source=(), rule='exact'):
        # Collection helpers return source bundles; G stores their bytes in its
        # established fields. Provenance always names a real final code path.
        destination = tuple(destination)
        if destination[0] == 'candidateSource':
            if destination[1] == 'absence':
                destination = ('absentFiles', 'candidate')
            else:
                path = destination[2]
                destination = ('candidateFiles' if path == 'go.mod' else 'candidateCIFiles', path)
        elif destination[0] == 'workflowSource':
            destination = (('absentFiles', 'workflow') if destination[1] == 'absence'
                           else ('workflowFiles', destination[2]))
        if 'testedSource' in destination:
            position = destination.index('testedSource')
            suffix = destination[position + 1:]
            destination = (*destination[:position],
                           'testedFiles' if suffix[0] == 'files' else 'testedAbsentFiles', *suffix[1:])
        self.m.require(len(self.projections) < 2048, 'bounded-projection-index')
        self.projections.append({'projectionPath': list(destination), 'captureIndex': index,
                                 'responsePath': list(source), 'rule': rule})

    def _repo(self):
        raw, index, _ = self._read(REPO)
        repository = self.p['repository']
        self.m.require(type(raw['id']) is int and raw['id'] == repository['repositoryId']
                       and raw['full_name'] == 'Hikyo-Org/Hikyo'
                       and type(raw['owner']['id']) is int and raw['owner']['id'] == repository['ownerId']
                       and raw['owner']['login'] == 'Hikyo-Org'
                       and raw['default_branch'] == 'main', 'fixed-repository-owner')
        self._map(('repository',), index, rule='fixed-repository-identity')
        return copy.deepcopy(repository)

    def _commit(self, sha, destination, *, public=True, compact=False):
        m = self.m
        m.require(m.sha(sha, 40), 'exact-commit-selector')
        raw, index, _ = self._read(REPO + '/commits/' + sha)
        m.require(raw['sha'] == sha and type(raw['parents']) is list and
                  1 <= len(raw['parents']) <= 2, 'whole-commit-identity')
        fields = {'sha': ('sha',), 'treeSHA': ('commit', 'tree', 'sha'),
                  'authorName': ('commit', 'author', 'name'),
                  'authorEmail': ('commit', 'author', 'email'), 'message': ('commit', 'message')}
        result = {}
        for name, path in fields.items():
            value = raw
            for part in path:
                value = value[part]
            result[name] = value
            if destination is not None and (not compact or name in ('sha', 'treeSHA')):
                self._map((*destination, name), index, path)
        result['parents'] = [row['sha'] for row in raw['parents']]
        result['verification'] = {k: raw['commit']['verification'][k] for k in ('verified', 'reason')}
        if destination is not None:
            self._map((*destination, 'parents'), index, ('parents',), 'ordered-parent-shas')
            if not compact:
                self._map((*destination, 'verification'), index, ('commit', 'verification'), 'verified-and-reason')
        if public:
            m.public_commit_checked(result, requires_dco=False)
        else:
            m.require(m.sha(result['treeSHA'], 40) and all(m.sha(p, 40) for p in result['parents']),
                      'tested-commit-tree-and-parents')
        return result

    def _tree(self, sha):
        raw, index, _ = self._read(REPO + '/git/trees/' + sha)
        m = self.m
        m.require(raw['sha'] == sha and raw['truncated'] is False and type(raw['tree']) is list
                  and len(raw['tree']) <= 2048, 'complete-nonrecursive-tree')
        names = []
        for row in raw['tree']:
            m.require(type(row['path']) is str and '/' not in row['path']
                      and row['path'] not in ('', '.', '..') and m.sha(row['sha'], 40)
                      and row['type'] in ('blob', 'tree', 'commit'), 'supported-tree-entry')
            names.append(row['path'])
        m.require(len(set(names)) == len(names), 'unique-tree-paths')
        return raw, index

    def _files(self, commit, paths, destination):
        m = self.m
        root, root_index = self._tree(commit['treeSHA'])
        m.require(not any(row['path'] == 'go.work' for row in root['tree']), 'root-go-work-absent')
        self._map((*destination, 'absence'), root_index, ('tree',), 'complete-go-work-absence')
        files = {}
        for path in sorted(paths):
            tree, index = root, root_index
            parts = path.split('/')
            for offset, part in enumerate(parts):
                matches = [(i, row) for i, row in enumerate(tree['tree']) if row['path'] == part]
                m.require(len(matches) == 1, 'required-file-present')
                position, row = matches[0]
                self._map((*destination, 'files', path), index, ('tree', position), 'source-tree-binding')
                if offset < len(parts) - 1:
                    m.require(row['type'] == 'tree' and row['mode'] == '040000', 'regular-source-directory')
                    tree, index = self._tree(row['sha'])
                    continue
                m.require(row['type'] == 'blob' and row['mode'] in ('100644', '100755'), 'regular-git-blob')
                blob, blob_index, _ = self._read(REPO + '/git/blobs/' + row['sha'])
                m.require(blob['sha'] == row['sha'] and blob['encoding'] == 'base64'
                          and type(blob['size']) is int and 0 <= blob['size'] <= 262144,
                          'bounded-source-blob')
                encoded = blob['content']
                m.require(type(encoded) is str and re.fullmatch(r'[A-Za-z0-9+/=\r\n]*', encoded)
                          is not None, 'supported-base64')
                compact = encoded.replace('\r\n', '').replace('\n', '')
                body = base64.b64decode(compact, validate=True)
                m.require(base64.b64encode(body).decode() == compact and len(body) == blob['size']
                          and hashlib.sha1(b'blob ' + str(len(body)).encode() + b'\0' + body).hexdigest()
                          == row['sha'], 'whole-git-blob-identity')
                files[path] = body.decode('utf-8')
                self._map((*destination, 'files', path), blob_index, ('content',), 'whole-git-base64-utf8')
        if 'go.mod' in files:
            directives = re.findall(r'(?m)^go\s+(\S+)\s*$', files['go.mod'])
            m.require(directives == ['1.27.3'] and re.search(r'(?m)^go 1\.27\.3$', files['go.mod'])
                      is not None, 'literal-go-1.27.3')
        return {'files': files, 'absence': {'commitSHA': commit['sha'], 'path': 'go.work',
                                           'gitTreeEntry': None}}

    def _pagination(self, headers, endpoint, page, more):
        links = {}
        for part in headers.get('link', '').split(',') if headers.get('link') else []:
            match = re.fullmatch(r'\s*<https://api\.github\.com/([^<>]+)>; rel="(next|prev|first|last)"\s*', part)
            self.m.require(match is not None and match[2] not in links, 'supported-complete-link-header')
            links[match[2]] = match[1]
        prefix = endpoint.rsplit('=', 1)[0] + '='
        for relation, target in links.items():
            self.m.require(target.startswith(prefix) and re.fullmatch(r'[1-3]', target[len(prefix):])
                           is not None, 'same-endpoint-page-links')
            number = int(target[len(prefix):])
            self.m.require((relation != 'first' or number == 1)
                           and (relation != 'prev' or number == page - 1)
                           and (relation != 'last' or (number > page if more else number == page)), 'consistent-page-links')
        self.m.require(links.get('next') == (prefix + str(page + 1) if more else None),
                       'exact-terminal-pagination')
        return links

    def _pr(self, number, destination, *, reread=False):
        raw, index, _ = self._read(f'{REPO}/pulls/{number}', cache=not reread)
        m = self.m
        repository = self.p['repository']['repositoryId']
        m.require(type(raw['number']) is int and raw['number'] == number
                  and type(raw['node_id']) is str and 1 <= len(raw['node_id']) <= 200
                  and all(type(raw[role]['repo']['id']) is int and raw[role]['repo']['id'] == repository
                          for role in ('head', 'base'))
                  and raw['base']['ref'] == 'main', 'same-repository-pr-main')
        self._map(destination, index, rule='supported-rest-pr-tuple')
        result = {'number': number, 'nodeId': raw['node_id'], 'headSHA': raw['head']['sha'],
                  'baseSHA': raw['base']['sha'], 'mergeSHA': raw['merge_commit_sha'],
                  'mergedAt': raw['merged_at'], 'state': raw['state'], 'merged': raw['merged'],
                  'commitCount': raw['commits']}
        m.require(all(m.sha(result[k], 40) for k in ('headSHA', 'baseSHA', 'mergeSHA'))
                  and type(result['merged']) is bool and type(result['commitCount']) is int
                  and 1 <= result['commitCount'] <= 250, 'supported-pr-shas-and-count')
        return result

    def _range(self, pr, destination):
        m = self.m
        count, pages = 0, []
        for page in range(1, 4):
            endpoint = f"{REPO}/pulls/{pr['number']}/commits?per_page=100&page={page}"
            rows, index, headers = self._read(endpoint)
            m.require(type(rows) is list and 1 <= len(rows) <= 100, 'whole-commit-range-page')
            count += len(rows)
            more = count < pr['commitCount']
            m.require(count <= pr['commitCount'] and (not more or len(rows) == 100), 'complete-range-pages')
            self._pagination(headers, endpoint, page, more)
            commits = []
            for i, row in enumerate(rows):
                commit = self._commit(row['sha'], (*destination, 'pages', page - 1, 'commits', i))
                detail, _, _ = self._read(REPO + '/commits/' + row['sha'])
                m.require(_same(row['commit'], detail['commit']) and _same(row['parents'], detail['parents']),
                          'list-detail-range-identity')
                m.public_commit_checked(commit, requires_dco=True)
                commits.append(commit)
            self._map((*destination, 'pages', page - 1), index, rule='whole-pr-range-page')
            pages.append({'endpoint': endpoint, 'page': page, 'perPage': 100,
                          'commits': commits, 'nextPage': page + 1 if more else None})
            if not more:
                break
        m.require(count == pr['commitCount'], 'all-source-range-pages')
        record = {'repositoryId': self.p['repository']['repositoryId'], 'number': pr['number'],
                  'baseSHA': pr['baseSHA'], 'headSHA': pr['headSHA'], 'total_count': count, 'pages': pages}
        associated = self._associated(pr)
        m.pr_commit_range_checked(record, associated)
        # Independently require the complete reachable ordered source range.
        seen = {pr['baseSHA']}
        for commit in [c for page in pages for c in page['commits']]:
            m.require(commit['sha'] not in seen and all(parent in seen for parent in commit['parents']),
                      'complete-topological-source-range')
            seen.add(commit['sha'])
        m.require(pr['headSHA'] in seen, 'source-range-reaches-head')
        return record

    def _associated(self, pr):
        return {'number': pr['number'], 'headSHA': pr['headSHA'],
                'headRepositoryId': self.p['repository']['repositoryId'], 'baseSHA': pr['baseSHA'],
                'baseRef': 'main', 'mergeSHA': pr['mergeSHA']}

    def _run_raw(self, run_id, attempt, *, reread=False, latest=False):
        endpoint = f'{REPO}/actions/runs/{run_id}'
        if not latest:
            endpoint += f'/attempts/{attempt}'
        raw, index, _ = self._read(endpoint, cache=not reread)
        self.m.require(type(raw['id']) is int and raw['id'] == run_id
                       and type(raw['run_attempt']) is int and raw['run_attempt'] == attempt,
                       'selected-run-attempt-unchanged')
        return raw, index

    def _run(self, pr, run_id, attempt, kind, destination):
        m = self.m
        raw, index = self._run_raw(run_id, attempt)
        latest, _ = self._run_raw(run_id, attempt, latest=True)
        fields = ('id', 'head_sha', 'head_branch', 'name', 'run_attempt', 'path', 'event',
                  'status', 'conclusion', 'created_at', 'updated_at')
        result = {key: raw[key] for key in fields}
        m.require(_same(result, {key: latest[key] for key in fields}), 'current-run-and-selected-attempt')
        caller = ('.github/workflows/ci-candidate.yml', 'pull_request') if kind == 'candidate' else (
            '.github/workflows/ci-merge-group.yml', 'merge_group')
        m.require((result['path'], result['event']) == caller and result['status'] == 'completed'
                  and result['conclusion'] == 'success'
                  and result['name'] == ('candidate-ci #' + str(pr['number']) if kind == 'candidate'
                                         else 'trusted-merge-ci')
                  and result['head_sha'] == pr['headSHA' if kind == 'candidate' else 'mergeSHA'],
                  'exact-complete-ci-role')
        m.require(m.instant(result['created_at']) <= m.instant(result['updated_at']) <= self.started,
                  'real-completed-run-times')
        m.require(type(result['head_branch']) is str and 1 <= len(result['head_branch']) <= 200
                  and (kind == 'candidate' or result['head_branch'] ==
                       f"gh-readonly-queue/main/pr-{pr['number']}-{pr['baseSHA']}"), 'single-pr-queue-branch')
        m.require(type(raw['repository']['id']) is int
                  and raw['repository']['id'] == self.p['repository']['repositoryId'], 'run-repository')
        rows = raw['pull_requests']
        m.require(type(rows) is list and len(rows) <= 1 and (kind != 'candidate' or len(rows) == 1),
                  'single-pr-run-association')
        # GitHub merge_group run metadata may expose an empty pull_requests array;
        # exact public commit identity/ordered parents and the single-PR queue
        # branch bind that supported case. Never accept conflicting associations.
        if rows:
            m.require(type(rows[0]['number']) is int and rows[0]['number'] == pr['number']
                      and rows[0]['head']['sha'] == pr['headSHA'] and rows[0]['base']['sha'] == pr['baseSHA'],
                      'run-exact-pr-pair')
        self._map(destination, index, rule='exact-selected-run')
        tested = self._commit(pr['mergeSHA'], (*destination, 'testedCommit'), public=(kind == 'workflow'), compact=True)
        m.require(tested['parents'] == [pr['baseSHA'], pr['headSHA']], 'ordered-two-parent-single-pr-merge')
        paths = self.p['trustedPaths'] if kind == 'workflow' else ['go.mod', *self.p['candidateCIPaths']]
        source = self._files(tested, paths, (*destination, 'testedSource'))
        result['apiPR'] = self._associated(pr)
        result['testedCommit'] = {key: tested[key] for key in ('sha', 'treeSHA', 'parents')}
        result['testedFileSHA256s'] = _pins(source['files'])
        result['testedAbsentFiles'] = source['absence']
        result['testedFiles'] = source['files']
        pages, count, total, names, ids = [], 0, None, [], []
        for page in range(1, 4):
            endpoint = f'{REPO}/actions/runs/{run_id}/attempts/{attempt}/jobs?per_page=100&page={page}'
            body, source_index, headers = self._read(endpoint)
            m.require(type(body['total_count']) is int and 1 <= body['total_count'] <= 200
                      and type(body['jobs']) is list and 1 <= len(body['jobs']) <= 100
                      and (total is None or total == body['total_count']), 'stable-complete-jobs-count')
            total = body['total_count']
            count += len(body['jobs'])
            more = count < total
            m.require(count <= total and (not more or len(body['jobs']) == 100), 'whole-job-page')
            self._pagination(headers, endpoint, page, more)
            jobs = []
            for row in body['jobs']:
                keys = ('id', 'run_id', 'run_attempt', 'head_sha', 'name', 'status', 'conclusion')
                job = {key: row[key] for key in keys}
                for key in ('id', 'run_id', 'run_attempt'):
                    _positive(m, job[key])
                m.require(job['run_id'] == run_id and job['run_attempt'] == attempt
                          and job['head_sha'] == result['head_sha'] and job['status'] == 'completed'
                          and type(job['name']) is str and job['name'] in self.p['requiredCIJobs'][kind]
                          and job['conclusion'] == self.p['requiredCIJobs'][kind][job['name']],
                          'exact-job-identity-and-outcome')
                names.append(job['name'])
                ids.append(job['id'])
                jobs.append(job)
            self._map((*destination, 'jobPages', page - 1), source_index, rule='whole-jobs-page')
            pages.append({'endpoint': endpoint, 'page': page, 'perPage': 100,
                          'total_count': total, 'jobs': jobs, 'nextPage': page + 1 if more else None})
            if not more:
                break
        m.require(count == total and len(set(names)) == len(names) and len(set(ids)) == len(ids)
                  and set(names) == set(self.p['requiredCIJobs'][kind]), 'complete-fixed-job-inventory')
        result['jobPages'] = pages
        return result

    def _timeline_time(self, value, *, git_date=False):
        m = self.m
        try:
            if git_date:
                # Native Git commit dates may have an explicit numeric offset;
                # parse for comparison without rewriting the captured row.
                m.require(type(value) is str and re.fullmatch(
                    r'[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9]{2}:[0-9]{2}:[0-9]{2}'
                    r'(?:\.[0-9]{1,6})?(?:Z|[+-](?:[01][0-9]|2[0-3]):[0-5][0-9])', value
                ) is not None, 'timeline-native-time')
                when = datetime.fromisoformat(value.replace('Z', '+00:00'))
            else:
                when = m.instant(value)
        except ValueError as error:
            raise ValueError('collector:timeline-native-time') from error
        m.require(when <= self.started, 'real-timeline-time')

    def _timeline_identity(self, row):
        """Validate supported native identity/time variants, never normalize rows."""
        m = self.m
        m.require(type(row) is dict and type(row.get('event')) is str,
                  'unsupported-timeline-event')
        event = row['event']
        m.require(event in TIMELINE_ISSUE_EVENTS or event in ('committed', 'reviewed', 'commented'),
                  'unsupported-timeline-event')
        if event == 'committed':
            identity = row.get('sha')
            m.require(m.sha(identity, 40), 'timeline-commit-identity')
            if 'id' in row:
                _positive(m, row['id'])
            namespace = 'commit'
            for role in ('author', 'committer'):
                person = row.get(role)
                m.require(type(person) is dict, 'timeline-native-time')
                self._timeline_time(person.get('date'), git_date=True)
        else:
            identity = row.get('id')
            _positive(m, identity)
            namespace = {'reviewed': 'review', 'commented': 'issue-comment'}.get(event, 'issue-event')
            self._timeline_time(row.get('submitted_at' if event == 'reviewed' else 'created_at'))
        node = row.get('node_id')
        m.require(type(node) is str and 1 <= len(node) <= 200, 'timeline-native-node-id')
        if event == 'reviewed' or 'commit_id' in row:
            commit = row.get('commit_id')
            m.require((event != 'reviewed' and commit is None) or m.sha(commit, 40),
                      'timeline-event-commit-sha')
        # Preserve the earlier bound on any supplied top-level created_at, too;
        # it never substitutes for a commit/review's required native timestamp.
        if event in ('committed', 'reviewed') and 'created_at' in row:
            self._timeline_time(row['created_at'])
        return ((namespace, identity), ('node', node))

    def _episode(self, number, merge_sha, run_id, attempt, destination):
        m = self.m
        pr = self._pr(number, (*destination, 'pullRequest'))
        m.require(pr['merged'] is True and pr['state'] == 'closed' and pr['mergeSHA'] == merge_sha
                  and m.instant(pr['mergedAt']) <= self.started, 'exact-post-merge-pr')
        public = self._commit(merge_sha, (*destination, 'publicCommit'))
        m.require(public['parents'] == [pr['baseSHA'], pr['headSHA']], 'public-ordered-merge-parents')
        timeline, pages = [], []
        for page in range(1, 4):
            endpoint = f'{REPO}/pulls/{number}/timeline?per_page=100&page={page}'
            rows, index, headers = self._read(endpoint)
            m.require(type(rows) is list and len(rows) <= 100, 'bounded-whole-timeline-page')
            more = bool(re.search(r'rel="next"', headers.get('link', '')))
            m.require(not more or len(rows) == 100, 'full-nonterminal-timeline-page')
            self._pagination(headers, endpoint, page, more)
            self._map((*destination, 'timelinePages', page - 1), index)
            pages.append(copy.deepcopy(rows))
            timeline.extend(rows)
            if not more:
                break
        m.require(not more, 'terminal-timeline-pagination')
        # Retain earlier events verbatim; no enqueue/removal causality is inferred.
        event_ids, merged = set(), []
        for row in timeline:
            identities = self._timeline_identity(row)
            m.require(not any(identity in event_ids for identity in identities),
                      'duplicate-timeline-native-identity')
            event_ids.update(identities)
            if row['event'] == 'merged':
                merged.append(row)
        m.require(len(merged) == 1, 'unique-unambiguous-merged-event')
        event = merged[0]
        _positive(m, event['id'])
        m.require(type(event['node_id']) is str and 1 <= len(event['node_id']) <= 200, 'merged-event-node-id')
        actor = self.p['ownerActor']
        m.require(type(event['actor']['id']) is int and event['actor']['id'] == actor['databaseId']
                  and event['actor']['login'] == actor['login'] and event['commit_id'] == merge_sha
                  and event['created_at'] == pr['mergedAt'], 'exact-owner-public-merged-event')
        cursor, seen_cursors, graphql_pages, graphql_events = None, set(), [], []
        for page in range(3):
            body = {'query': GRAPHQL, 'variables': {'number': number, 'cursor': cursor}}
            raw, index, _ = self._read('graphql', body=body)
            m.require(type(raw) is dict and set(raw) == {'data'}, 'graphql-data-without-errors')
            repo = raw['data']['repository']
            m.require(type(repo['databaseId']) is int and repo['databaseId'] == self.p['repository']['repositoryId']
                      and repo['nameWithOwner'] == 'Hikyo-Org/Hikyo' and repo['owner']['login'] == 'Hikyo-Org'
                      and type(repo['owner']['databaseId']) is int
                      and repo['owner']['databaseId'] == self.p['repository']['ownerId'], 'graphql-fixed-repository')
            node = repo['pullRequest']
            m.require(type(node['number']) is int and node['number'] == number and node['id'] == pr['nodeId']
                      and node['state'] == 'MERGED' and node['merged'] is True
                      and node['mergedAt'] == pr['mergedAt'] and node['headRefOid'] == pr['headSHA']
                      and node['baseRefOid'] == pr['baseSHA'] and node['mergeCommit']['oid'] == merge_sha,
                      'rest-graphql-exact-pr-tuple')
            connection = node['timelineItems']
            m.require(type(connection['nodes']) is list and len(connection['nodes']) <= 100,
                      'bounded-graphql-merged-events')
            info = connection['pageInfo']
            m.exact(info, {'hasNextPage', 'endCursor'})
            m.require(type(info['hasNextPage']) is bool, 'explicit-graphql-terminal-page')
            graphql_events.extend(connection['nodes'])
            graphql_pages.append(copy.deepcopy(raw))
            self._map((*destination, 'graphqlPages', page), index)
            if not info['hasNextPage']:
                break
            cursor = info['endCursor']
            m.require(len(connection['nodes']) == 100 and type(cursor) is str
                      and 1 <= len(cursor) <= 1024 and cursor not in seen_cursors, 'complete-graphql-cursors')
            seen_cursors.add(cursor)
        m.require(info['hasNextPage'] is False and len(graphql_events) == 1, 'one-complete-graphql-merged-event')
        gql = graphql_events[0]
        m.require(gql['id'] == event['node_id'] and gql['createdAt'] == event['created_at']
                  and gql['commit']['oid'] == merge_sha and gql['actor']['login'] == actor['login']
                  and type(gql['actor']['databaseId']) is int and gql['actor']['databaseId'] == actor['databaseId'],
                  'rest-graphql-exact-owner-event')
        run = self._run(pr, run_id, attempt, 'workflow', (*destination, 'queueRun'))
        m.require(run['testedCommit'] == {key: public[key] for key in ('sha', 'treeSHA', 'parents')},
                  'identical-public-and-tested-sha-tree-ordered-parents')
        source_range = self._range(pr, (*destination, 'sourceRange'))
        return {'mode': MODE, 'pullRequest': pr, 'publicCommit': public, 'timelinePages': pages,
                'graphqlPages': graphql_pages, 'queueRun': run, 'sourceRange': source_range}

    def _lineage(self, tip, anchor, destination):
        m = self.m
        reverse, cursor, seen = [], tip, set()
        while cursor['sha'] != anchor:
            m.require(len(reverse) < release_policy.FIRST_PARENT_COMMITS and cursor['sha'] not in seen, 'bounded-signed-first-parent-lineage')
            seen.add(cursor['sha'])
            reverse.append(cursor)
            cursor = self._commit(cursor['parents'][0], None)
        result = {'anchor': cursor, 'commits': list(reversed(reverse))}
        self._commit(cursor['sha'], (*destination, 'anchor'))
        for position, commit in enumerate(result['commits']):
            self._commit(commit['sha'], (*destination, 'commits', position))
        return result

    def _reread(self, endpoint, *, body=None):
        before, _, before_headers = self._read(endpoint, body=body)
        after, index, after_headers = self._read(endpoint, body=body, cache=False)
        self.m.require(_same(before, after), 'stable-whole-pointer-pr-event-and-run-readback')
        if '/timeline?per_page=100&page=' in endpoint:
            page = int(endpoint.rsplit('=', 1)[1])
            more = bool(re.search(r'rel="next"', before_headers.get('link', '')))
            before_links = self._pagination(before_headers, endpoint, page, more)
            after_links = self._pagination(after_headers, endpoint, page, more)
            self.m.require(before_links == after_links, 'stable-complete-timeline-pagination')
        position = len(self.readbacks)
        self.readbacks.append({'captureIndex': index, 'endpoint': endpoint,
                               'semanticSHA256': _sha(self.m.canonical(after))})
        self._map(('finalReadbacks', position, 'semanticSHA256'), index, rule='canonical-response-sha256')

    def _stable_episode(self, pr_number, run_id, attempt):
        self._reread(f'{REPO}/pulls/{pr_number}')
        self._reread(f'{REPO}/actions/runs/{run_id}/attempts/{attempt}')
        self._reread(f'{REPO}/actions/runs/{run_id}')
        requests = [copy.deepcopy(o['request']) for o in self.originals
                    if (o['request']['endpoint'] == 'graphql'
                        and o['request']['body']['variables']['number'] == pr_number)
                    or o['request']['endpoint'].startswith(f'{REPO}/pulls/{pr_number}/timeline?')]
        for request in requests:
            self._reread(request['endpoint'], body=request['body'])

    def _finish(self, schema, selection, code, *, change_review=None):
        finished = self._time()
        code['finalReadbacks'] = copy.deepcopy(self.readbacks)
        for projection in self.projections:
            projected = code
            for part in projection['projectionPath']:
                projected = projected[part]
        if schema == PAIR_SCHEMA:
            code['observedAt'] = self.m.stamp(finished)
        self.m.canonical(code)
        return {'schema': schema, **FLAGS, 'startedAt': self.m.stamp(self.started),
                'finishedAt': self.m.stamp(finished), 'selection': copy.deepcopy(selection),
                'code': code, 'changeReview': copy.deepcopy(change_review),
                'originals': copy.deepcopy(self.originals), 'projectionPaths': copy.deepcopy(self.projections),
                'requestCount': len(self.originals), 'rawResponseBytes': self.total_bytes}

    def collect_release(self, selection, change_review):
        m = self.m
        _selection(m, selection, 'release')
        m.require(selection['historicalAnchorSHA'] == m.BOOTSTRAP_1017['mergeSHA'],
                  'literal-historical-1017-anchor')
        self._start()
        repository = self._repo()
        episode = self._episode(selection['prNumber'], selection['mergeSHA'], selection['runId'],
                                selection['attempt'], ('episode',))
        files = self._files(episode['publicCommit'], self.p['trustedPaths'], ('releaseSource',))
        lineage = self._lineage(episode['publicCommit'], selection['historicalAnchorSHA'], ('historicalLineage',))
        _review_checked(m, change_review, files['files'], self.p)
        m.require(m.instant(change_review['reviewedAt']) <= self.started
                  and change_review['previousCommitSHA'] != selection['mergeSHA'], 'prior-reviewed-source-baseline')
        baseline = self._commit(change_review['previousCommitSHA'], ('reviewBaselineCommit',))
        baseline_files = self._files(baseline, self.p['trustedPaths'], ('reviewBaselineSource',))
        m.require(_pins(baseline_files['files']) == change_review['previousFileSHA256s'],
                  'whole-reviewed-baseline-vector')
        m.require(baseline['sha'] in [lineage['anchor']['sha'], *[c['sha'] for c in lineage['commits']]],
                  'review-baseline-in-complete-historical-lineage')
        self._stable_episode(selection['prNumber'], selection['runId'], selection['attempt'])
        code = {'repository': repository, 'episode': episode, 'releaseSource': files,
                'historicalLineage': lineage, 'reviewBaselineCommit': baseline,
                'reviewBaselineSource': baseline_files}
        return self._finish(RELEASE_SCHEMA, selection, code, change_review=change_review)

    def collect_pair(self, selection, manifest, release):
        m = self.m
        _selection(m, selection, 'pair')
        checked = release_checked(m, release)
        _manifest_checked(m, manifest, checked['releaseSource']['files'], self.p)
        self._start()
        repository = self._repo()
        ref, ref_index, _ = self._read(REPO + '/git/ref/heads/main')
        m.require(ref['ref'] == 'refs/heads/main' and ref['object']['type'] == 'commit'
                  and ref['object']['sha'] == selection['mainSHA'], 'explicit-current-main-pointer')
        self._map(('workflowMainRef',), ref_index, rule='exact-current-main')
        main = self._episode(selection['mainPRNumber'], selection['mainSHA'], selection['mainRunId'],
                             selection['mainAttempt'], ('mainEpisode',))
        current = main['publicCommit']
        lineage = self._lineage(current, release['selection']['mergeSHA'], ('releaseLineage',))
        m.require(_same(lineage['anchor'], checked['episode']['publicCommit']), 'release-anchor-whole-commit-identity')
        workflow = self._files(current, self.p['trustedPaths'], ('workflowSource',))
        m.require(_pins(workflow['files']) == _pins(checked['releaseSource']['files']), 'main-covered-by-release-vector')
        candidate_pr = self._pr(selection['candidatePRNumber'], ('candidatePR',))
        m.require(candidate_pr['state'] == 'open' and candidate_pr['merged'] is False
                  and candidate_pr['mergedAt'] is None and candidate_pr['headSHA'] == selection['candidateHeadSHA']
                  and candidate_pr['baseSHA'] == current['sha'], 'fresh-exact-open-candidate-main-pair')
        candidate = self._commit(selection['candidateHeadSHA'], ('candidateCommit',))
        m.public_commit_checked(candidate, requires_dco=True)
        source = self._files(candidate, ['go.mod', *self.p['candidateCIPaths']], ('candidateSource',))
        candidate_ci = {path: source['files'][path] for path in self.p['candidateCIPaths']}
        m.require(_pins(candidate_ci) == manifest['candidateCIFileSHA256s']
                  and _sha(source['files']['go.mod'].encode()) == manifest['candidateFileSHA256s']['go.mod'],
                  'candidate-source-bound-separately-from-approved-ci')
        run = self._run(candidate_pr, selection['candidateRunId'], selection['candidateAttempt'],
                        'candidate', ('ciRuns', 'candidate'))
        m.require(run['testedFiles'] == source['files'], 'candidate-head-and-tested-merge-source-equality')
        source_range = self._range(candidate_pr, ('prCommitRanges', 'candidate'))
        m.require(_same(source_range['pages'][-1]['commits'][-1], candidate), 'candidate-range-tip')
        m.require(main['queueRun']['testedFiles'] == workflow['files'], 'main-public-tested-approved-source-equality')
        self._reread(REPO + '/git/ref/heads/main')
        self._reread(f"{REPO}/pulls/{selection['candidatePRNumber']}")
        for latest in (False, True):
            endpoint = f"{REPO}/actions/runs/{selection['candidateRunId']}"
            if not latest:
                endpoint += f"/attempts/{selection['candidateAttempt']}"
            self._reread(endpoint)
        self._stable_episode(selection['mainPRNumber'], selection['mainRunId'], selection['mainAttempt'])
        code = {'schema': 'hikyo-158/next-code-evidence/v5', 'repository': repository,
                'repositoryId': repository['repositoryId'], 'ownerId': repository['ownerId'],
                'candidateCommit': candidate, 'workflowCommit': current,
                'candidateFiles': {'go.mod': source['files']['go.mod']}, 'candidateCIFiles': candidate_ci,
                'workflowFiles': workflow['files'],
                'absentFiles': {'candidate': source['absence'], 'workflow': workflow['absence']},
                'ciRuns': {'candidate': run, 'workflow': main['queueRun']},
                'prCommitRanges': {'candidate': source_range, 'workflow': main['sourceRange']},
                'candidatePR': candidate_pr, 'mainEpisode': main, 'releaseLineage': lineage,
                'workflowMainRef': {'endpoint': REPO + '/git/ref/heads/main',
                                    'observedAt': self.originals[ref_index]['receivedAt'],
                                    'ref': ref['ref'], 'object': {'type': 'commit', 'sha': current['sha']}},
                'sourceManifestSHA256': _sha(m.canonical(manifest)),
                'releaseCollectionSHA256': collection_digest(m, release)}
        return self._finish(PAIR_SCHEMA, selection, code)


def _review_checked(m, review, files, policy):
    m.canonical(review)
    m.exact(review, {'schema', 'previousCommitSHA', 'previousFileSHA256s', 'currentFileSHA256s',
                     'changedPaths', 'reviews', 'originals', 'reviewedAt'})
    m.require(review['schema'] == 'hikyo-158/trusted-source-change-review/v1'
              and m.sha(review['previousCommitSHA'], 40), 'whole-source-change-review')
    for key in ('previousFileSHA256s', 'currentFileSHA256s'):
        m.exact(review[key], policy['trustedPaths'])
        m.require(all(m.sha(v) for v in review[key].values()), 'complete-review-vectors')
    m.require(review['currentFileSHA256s'] == _pins(files), 'review-exact-current-source')
    changed = sorted(path for path in policy['trustedPaths']
                     if review['previousFileSHA256s'][path] != review['currentFileSHA256s'][path])
    m.require(_same(review['changedPaths'], changed), 'complete-trusted-source-diff')
    m.exact(review['reviews'], changed)
    m.require(type(review['originals']) is dict and 1 <= len(review['originals']) <= 200, 'whole-review-original-index')
    total = 0
    for name, original in review['originals'].items():
        m.require(type(name) is str and 1 <= len(name) <= 200, 'review-original-identity')
        m.exact(original, {'body', 'bodySHA256', 'bodyBytes', 'observedAt'})
        m.require(type(original['body']) is str, 'whole-review-original-body')
        raw = original['body'].encode('utf-8')
        total += len(raw)
        m.require(type(original['bodyBytes']) is int and 1 <= len(raw) <= 2_000_000
                  and len(raw) == original['bodyBytes'] and _sha(raw) == original['bodySHA256']
                  and total <= 24_000_000 and m.instant(original['observedAt']) <= m.instant(review['reviewedAt']),
                  'review-original-whole-byte-and-time-identity')
    for path, row in review['reviews'].items():
        m.exact(row, {'previousSHA256', 'currentSHA256', 'originalIds'})
        m.require(row['previousSHA256'] == review['previousFileSHA256s'][path]
                  and row['currentSHA256'] == review['currentFileSHA256s'][path]
                  and type(row['originalIds']) is list and 1 <= len(row['originalIds']) <= 200
                  and len(set(row['originalIds'])) == len(row['originalIds'])
                  and all(name in review['originals'] for name in row['originalIds']),
                  'every-changed-path-reviewed-provenance')


def _manifest_checked(m, manifest, files, policy):
    m.canonical(manifest)
    keys = {'schema', 'modelSHA256', 'testSHA256', 'frozenNextHelpers', 'candidateFileSHA256s',
            'candidateCIFileSHA256s', 'workflowFileSHA256s', 'requiredCIJobs', 'configurationSHA256',
            'resourceConfigurationSHA256', 'policySHA256', 'releaseSHA256', 'ownerApprovalSHA256',
            'rootAdoptionSHA256', 'historicalHistorySHA256'}
    m.exact(manifest, keys)
    m.require(manifest['schema'] == 'hikyo-158/future-fixture-source-manifest/v4', 'source-manifest-v4')
    for key in keys:
        if key.endswith('SHA256'):
            m.require(m.sha(manifest[key]), 'source-manifest-digest')
    m.exact(manifest['candidateFileSHA256s'], {'go.mod'})
    m.require(m.sha(manifest['candidateFileSHA256s']['go.mod']), 'candidate-go-mod-pin')
    m.require(_same(manifest['requiredCIJobs'], policy['requiredCIJobs'])
              and _same(manifest['frozenNextHelpers'], dict(release_policy.FROZEN))
              and manifest['workflowFileSHA256s'] == _pins(files)
              and manifest['candidateCIFileSHA256s'] == {path: _sha(files[path].encode())
                                                       for path in policy['candidateCIPaths']},
              'fixed-policy-jobs-helpers-and-approved-source-vectors')


def _replay(m, collection, callback):
    m.exact(collection, {'schema', *FLAGS, 'startedAt', 'finishedAt', 'selection', 'code',
                         'changeReview', 'originals', 'projectionPaths', 'requestCount', 'rawResponseBytes'})
    originals = collection['originals']
    m.require(type(originals) is list and 1 <= len(originals) <= release_policy.COLLECTION_REQUESTS, 'bounded-whole-originals')
    times = [collection['startedAt']]
    total = 0
    for i, original in enumerate(originals):
        m.exact(original, {'captureIndex', 'request', 'sentAt', 'receivedAt', 'status', 'headers',
                           'body', 'bodyBytes', 'bodySHA256', 'semanticSHA256'})
        m.require(type(original['body']) is str, 'whole-original-body')
        total += len(original['body'].encode())
        m.require(total <= release_policy.COLLECTION_RAW_BYTES, 'whole-original-aggregate-byte-budget')
        m.require(type(original['captureIndex']) is int and original['captureIndex'] == i
                  and type(original['body']) is str and type(original['bodyBytes']) is int
                  and len(original['body'].encode()) == original['bodyBytes']
                  and _sha(original['body'].encode()) == original['bodySHA256']
                  and _sha(m.canonical(m.strict_json(original['body'].encode()))) == original['semanticSHA256'],
                  'whole-original-byte-integrity')
        times.extend([original['sentAt'], original['receivedAt']])
    times.append(collection['finishedAt'])
    clock_values, responses = iter(times), iter(originals)
    used = 0

    def transport(request):
        nonlocal used
        original = next(responses, None)
        m.require(original is not None and _same(request, original['request']), 'original-request-correspondence')
        used += 1
        return {'status': original['status'], 'headers': copy.deepcopy(original['headers']),
                'body': original['body'].encode('utf-8')}

    def explicit_clock():
        value = next(clock_values, None)
        m.require(value is not None, 'complete-capture-clock')
        return m.instant(value)

    reproduced = callback(Collector(m, transport, explicit_clock))
    m.require(used == len(originals) and next(clock_values, None) is None
              and _same(collection, reproduced), 'whole-original-semantic-projection-replay')
    return reproduced['code']


def collection_digest(m, collection):
    """Merkle-style integrity reference; whole bytes are still checked by replay."""
    leaves = []
    for key in sorted(collection):
        value = collection[key]
        if key in ('originals', 'projectionPaths'):
            leaves.append([key, [_sha(m.canonical(row)) for row in value]])
        else:
            leaves.append([key, _sha(m.canonical(value))])
    # The fixed request budget also bounds this small digest index.
    return _sha(m.canonical(leaves))


def release_checked(m, release):
    m.require(type(release) is dict and release.get('schema') == RELEASE_SCHEMA, 'release-collection-schema')
    code = _replay(m, release, lambda c: c.collect_release(release['selection'], release['changeReview']))
    m.require(m.instant(release['changeReview']['reviewedAt']) <= m.instant(release['finishedAt']),
              'release-review-before-evidence-completion')
    return code


def pair_checked(m, proof, manifest, release, explicit_now):
    """Pure integrity/freshness check. Status/adoption/history are separate gates."""
    m.clock(explicit_now)
    m.require(type(proof) is dict and proof.get('schema') == PAIR_SCHEMA, 'pair-collection-schema')
    code = _replay(m, proof, lambda c: c.collect_pair(proof['selection'], manifest, release))
    for observed in (proof['startedAt'], proof['finishedAt'], code['workflowMainRef']['observedAt']):
        m.require(timedelta(0) <= explicit_now - m.instant(observed) <= timedelta(seconds=release_policy.COLLECTION_SECONDS),
                  'fresh-complete-current-main-and-candidate-proof')
    return {'candidate': code['candidateCommit']['sha'], 'workflow': code['workflowCommit']['sha']}


def pair_code_checked(m, code, manifest, release, collection, explicit_now):
    """G adapter: bind its inner code packet to the complete original collection."""
    m.require(type(collection) is dict and _same(code, collection.get('code')), 'code-packet-whole-collection-binding')
    return pair_checked(m, collection, manifest, release, explicit_now)
