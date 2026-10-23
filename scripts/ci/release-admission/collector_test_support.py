"""Synthetic offline provider originals; real collector and G semantic validators.

This module supplies no production transport, authentication or release approval.
"""
import base64
import copy
from datetime import timedelta
import hashlib
import importlib.util
from pathlib import Path

import bounded
import release_collector as collector
import release_policy as policy

NOW = bounded.instant('2026-10-23T12:00:00Z')
MERGED_AT = '2026-10-23T10:00:00Z'
RUN_CREATED = '2026-10-23T09:00:00Z'
RUN_COMPLETED = '2026-10-23T11:00:00Z'


def model():
    """Load the parent's portable successor; never read the frozen source tree."""
    spec = importlib.util.spec_from_file_location(
        'collector_synthetic_core', Path(__file__).with_name('core') / 'next_fixture_model.source-only.py')
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


def sha(label):
    return hashlib.sha1(label.encode()).hexdigest()


def file_bytes(version):
    return {path: ('module synthetic.invalid/hikyo\n\ngo 1.27.3\n' if path == 'go.mod'
                   else f'synthetic {version} {path}\n') for path in policy.TRUSTED_FILES}


class Fixture:
    """Two independently selectable releases and candidate pairs, one code source."""
    def __init__(self, version=1, *, descendant=True):
        self.m = model()
        self.responses, self.graphql, self.calls = {}, {}, []
        self.files = file_bytes(version)
        self.old_files = file_bytes(0)
        self.version = version
        self.anchor = self.commit('historical-anchor', [sha('earlier')], self.old_files,
                                  oid=self.m.BOOTSTRAP_1017['mergeSHA'])
        self.release_head = self.commit(f'release-head-{version}', [self.anchor], self.files)
        self.release_sha = self.commit(f'release-merge-{version}', [self.anchor, self.release_head], self.files)
        self.release_pr = 2000 + version
        self.release_run = 3000 + version
        self.episode(self.release_pr, self.release_head, self.anchor, self.release_sha, self.release_run)
        self.main_pr = 4000 + version if descendant else self.release_pr
        self.main_run = 5000 + version if descendant else self.release_run
        if descendant:
            head = self.commit(f'main-head-{version}', [self.release_sha], self.files)
            self.main_sha = self.commit(f'main-merge-{version}', [self.release_sha, head], self.files)
            self.episode(self.main_pr, head, self.release_sha, self.main_sha, self.main_run)
        else:
            self.main_sha = self.release_sha
        self.candidate_pr = 969
        candidate_files = copy.deepcopy(self.files)
        candidate_files['go.mod'] += '// separate candidate dependency vector\n'
        self.candidate_sha = self.commit(f'candidate-head-{version}', [self.main_sha], candidate_files)
        self.candidate_merge = self.commit(f'candidate-tested-{version}', [self.main_sha, self.candidate_sha], candidate_files)
        self.candidate_run = 6000 + version
        self.pr(self.candidate_pr, self.candidate_sha, self.main_sha, self.candidate_merge, merged=False)
        self.run(self.candidate_pr, self.candidate_sha, self.main_sha, self.candidate_merge,
                 self.candidate_run, 'candidate')
        self.put(collector.REPO, {'id': policy.REPOSITORY['repositoryId'], 'full_name': 'Hikyo-Org/Hikyo',
                                'owner': {'id': policy.REPOSITORY['ownerId'], 'login': 'Hikyo-Org'},
                                'default_branch': 'main'})
        self.put(collector.REPO + '/git/ref/heads/main', {'ref': 'refs/heads/main',
                                                      'object': {'type': 'commit', 'sha': self.main_sha}})
        self.release_selection = {'prNumber': self.release_pr, 'mergeSHA': self.release_sha,
                                  'runId': self.release_run, 'attempt': 1, 'historicalAnchorSHA': self.anchor}
        self.pair_selection = {'candidatePRNumber': self.candidate_pr, 'candidateHeadSHA': self.candidate_sha,
                               'candidateRunId': self.candidate_run, 'candidateAttempt': 1,
                               'mainSHA': self.main_sha, 'mainPRNumber': self.main_pr,
                               'mainRunId': self.main_run, 'mainAttempt': 1}
        pins = collector._pins(self.files)
        old = collector._pins(self.old_files)
        changed = sorted(path for path in pins if pins[path] != old[path])
        original = f'Explicit synthetic review of complete trusted diff for fixture {version}.\n'
        self.review = {'schema': 'hikyo-158/trusted-source-change-review/v1',
                       'previousCommitSHA': self.anchor, 'previousFileSHA256s': old,
                       'currentFileSHA256s': pins, 'changedPaths': changed,
                       'reviews': {path: {'previousSHA256': old[path], 'currentSHA256': pins[path],
                                          'originalIds': ['review-original']} for path in changed},
                       'originals': {'review-original': {'body': original, 'bodyBytes': len(original.encode()),
                                                         'bodySHA256': collector._sha(original.encode()),
                                                         'observedAt': '2026-10-23T11:30:00Z'}},
                       'reviewedAt': '2026-10-23T11:30:00Z'}
        self.manifest = {'schema': 'hikyo-158/future-fixture-source-manifest/v4',
                         'modelSHA256': '1' * 64, 'testSHA256': '2' * 64,
                         'frozenNextHelpers': dict(policy.FROZEN),
                         'candidateFileSHA256s': {'go.mod': collector._sha(candidate_files['go.mod'].encode())},
                         'candidateCIFileSHA256s': {path: pins[path] for path in policy.CANDIDATE_CI_FILES},
                         'workflowFileSHA256s': pins,
                         'requiredCIJobs': {k: dict(v) for k, v in policy.REQUIRED_CI_JOBS.items()},
                         **{key: bounded.digest(key) for key in (
                             'configurationSHA256', 'resourceConfigurationSHA256', 'policySHA256',
                             'releaseSHA256', 'ownerApprovalSHA256', 'rootAdoptionSHA256', 'historicalHistorySHA256')}}

    def put(self, endpoint, value, headers=None):
        self.responses[endpoint] = {'status': 200, 'headers': {'content-type': 'application/json', **(headers or {})},
                                    'body': bounded.canonical(value)}

    def read_value(self, endpoint):
        return bounded.strict_json(self.responses[endpoint]['body'])

    def tree(self, files):
        root = {}
        for path, content in files.items():
            current = root
            parts = path.split('/')
            for part in parts[:-1]:
                current = current.setdefault(part, {})
            current[parts[-1]] = content

        def build(node):
            entries = []
            for name, child in sorted(node.items()):
                if type(child) is dict:
                    oid = build(child)
                    mode, kind = '040000', 'tree'
                else:
                    body = child.encode()
                    oid = hashlib.sha1(b'blob ' + str(len(body)).encode() + b'\0' + body).hexdigest()
                    self.put(collector.REPO + '/git/blobs/' + oid,
                             {'sha': oid, 'encoding': 'base64', 'content': base64.b64encode(body).decode(), 'size': len(body)})
                    mode, kind = '100644', 'blob'
                entries.append({'path': name, 'mode': mode, 'type': kind, 'sha': oid})
            body = b''.join((row['mode'].lstrip('0') + ' ' + row['path']).encode() + b'\0'
                            + bytes.fromhex(row['sha']) for row in entries)
            oid = hashlib.sha1(b'tree ' + str(len(body)).encode() + b'\0' + body).hexdigest()
            self.put(collector.REPO + '/git/trees/' + oid, {'sha': oid, 'truncated': False, 'tree': entries})
            return oid
        return build(root)

    def commit(self, label, parents, files, *, oid=None):
        oid = oid or sha(label)
        self.put(collector.REPO + '/commits/' + oid,
                 {'sha': oid, 'parents': [{'sha': p} for p in parents],
                  'commit': {'tree': {'sha': self.tree(files)},
                             'author': {'name': 'Synthetic Author', 'email': 'synthetic@example.invalid'},
                             'message': label + '\n\nSigned-off-by: Synthetic Author <synthetic@example.invalid>\n',
                             'verification': {'verified': True, 'reason': 'valid'}}})
        return oid

    def pr(self, number, head, base, merge, *, merged=True, commits=None):
        repo = {'id': policy.REPOSITORY['repositoryId']}
        self.put(f'{collector.REPO}/pulls/{number}',
                 {'number': number, 'node_id': f'PR_{number}', 'state': 'closed' if merged else 'open',
                  'merged': merged, 'merged_at': MERGED_AT if merged else None, 'merge_commit_sha': merge,
                  'commits': len(commits) if commits else 1,
                  'head': {'sha': head, 'repo': repo}, 'base': {'sha': base, 'ref': 'main', 'repo': repo}})
        self.put(f'{collector.REPO}/pulls/{number}/commits?per_page=100&page=1',
                 [self.read_value(collector.REPO + '/commits/' + oid) for oid in (commits or [head])])

    def run(self, number, head, base, merge, run_id, kind):
        run = {'id': run_id, 'run_attempt': 1, 'head_sha': head if kind == 'candidate' else merge,
               'head_branch': 'synthetic-candidate' if kind == 'candidate' else f'gh-readonly-queue/main/pr-{number}-{base}',
               'name': f'candidate-ci #{number}' if kind == 'candidate' else 'trusted-merge-ci',
               'path': '.github/workflows/ci-candidate.yml' if kind == 'candidate' else '.github/workflows/ci-merge-group.yml',
               'event': 'pull_request' if kind == 'candidate' else 'merge_group',
               'status': 'completed', 'conclusion': 'success', 'created_at': RUN_CREATED, 'updated_at': RUN_COMPLETED,
               'repository': {'id': policy.REPOSITORY['repositoryId']},
               'pull_requests': [{'number': number, 'head': {'sha': head}, 'base': {'sha': base}}]}
        self.put(f'{collector.REPO}/actions/runs/{run_id}', run)
        self.put(f'{collector.REPO}/actions/runs/{run_id}/attempts/1', run)
        jobs = [{'id': run_id * 100 + i, 'run_id': run_id, 'run_attempt': 1, 'head_sha': run['head_sha'],
                 'name': name, 'status': 'completed', 'conclusion': result}
                for i, (name, result) in enumerate(policy.REQUIRED_CI_JOBS[kind].items(), 1)]
        self.put(f'{collector.REPO}/actions/runs/{run_id}/attempts/1/jobs?per_page=100&page=1',
                 {'total_count': len(jobs), 'jobs': jobs})

    def episode(self, number, head, base, merge, run_id):
        self.pr(number, head, base, merge)
        self.run(number, head, base, merge, run_id, 'workflow')
        api = 'https://api.github.com/' + collector.REPO
        event = {'id': number * 100, 'node_id': f'ME_{number}', 'event': 'merged', 'commit_id': merge,
                 'url': f'{api}/issues/events/{number * 100}', 'commit_url': f'{api}/commits/{merge}',
                 'actor': {'id': policy.OWNER_ACTOR['databaseId'], 'login': policy.OWNER_ACTOR['login']},
                 'created_at': MERGED_AT, 'performed_via_github_app': None}
        earlier = {'id': number * 100 - 1, 'node_id': f'REM_{number}', 'event': 'removed_from_merge_queue',
                   'url': f'{api}/issues/events/{number * 100 - 1}', 'commit_id': None, 'commit_url': None,
                   'actor': {'id': 123, 'login': 'synthetic-bot'}, 'created_at': '2026-10-23T09:59:00Z',
                   'performed_via_github_app': None}
        self.put(f'{collector.REPO}/pulls/{number}/timeline?per_page=100&page=1', [earlier, event])
        self.graphql[(number, None)] = {'data': {'repository': {
            'databaseId': policy.REPOSITORY['repositoryId'], 'nameWithOwner': 'Hikyo-Org/Hikyo',
            'owner': {'databaseId': policy.REPOSITORY['ownerId'], 'login': 'Hikyo-Org'},
            'pullRequest': {'id': f'PR_{number}', 'number': number, 'state': 'MERGED', 'merged': True,
                            'mergedAt': MERGED_AT, 'headRefOid': head, 'baseRefOid': base, 'mergeCommit': {'oid': merge},
                            'timelineItems': {'nodes': [{'id': event['node_id'], 'createdAt': MERGED_AT,
                                                         'actor': {'login': policy.OWNER_ACTOR['login'],
                                                                   'databaseId': policy.OWNER_ACTOR['databaseId']},
                                                         'commit': {'oid': merge}}],
                                              'pageInfo': {'hasNextPage': False, 'endCursor': None}}}}}}

    def timeline_variants(self, number):
        """Whole synthetic rows in GitHub's distinct native timeline shapes."""
        pr = self.read_value(f'{collector.REPO}/pulls/{number}')
        head = pr['head']['sha']
        commit = self.read_value(collector.REPO + '/commits/' + head)
        user = {'login': 'synthetic-commenter', 'id': 123, 'node_id': 'U_synthetic'}
        api = 'https://api.github.com/' + collector.REPO
        html = 'https://github.com/Hikyo-Org/Hikyo'
        # Review IDs and issue-comment IDs have separate native namespaces.
        identifier = number * 100 - 2
        return [
            {'event': 'committed', 'sha': head, 'node_id': f'C_{head}',
             'url': f'{api}/git/commits/{head}', 'html_url': f'{html}/commit/{head}',
             'author': {'name': 'Synthetic Author', 'email': 'synthetic@example.invalid',
                        'date': '2026-10-23T08:00:00-01:00'},
             'committer': {'name': 'Synthetic Committer', 'email': 'committer@example.invalid',
                           'date': '2026-10-23T10:15:00+01:00'},
             'tree': {'sha': commit['commit']['tree']['sha'],
                      'url': f"{api}/git/trees/{commit['commit']['tree']['sha']}"},
             'message': commit['commit']['message'],
             'parents': [{'sha': row['sha'], 'url': f"{api}/git/commits/{row['sha']}"}
                         for row in commit['parents']],
             'verification': {'verified': False, 'reason': 'unsigned',
                              'signature': None, 'payload': None, 'verified_at': None}},
            {'event': 'reviewed', 'id': identifier, 'node_id': f'PRR_{number}',
             'user': user, 'body': 'Synthetic comment review, no authority.',
             'state': 'commented', 'commit_id': head, 'submitted_at': '2026-10-23T09:30:00Z',
             'html_url': f'{html}/pull/{number}#pullrequestreview-{identifier}',
             'pull_request_url': f'{api}/pulls/{number}', 'author_association': 'NONE',
             '_links': {'html': {'href': f'{html}/pull/{number}#pullrequestreview-{identifier}'},
                        'pull_request': {'href': f'{api}/pulls/{number}'}}},
            {'event': 'commented', 'id': identifier, 'node_id': f'IC_{number}',
             'url': f'{api}/issues/comments/{identifier}',
             'html_url': f'{html}/pull/{number}#issuecomment-{identifier}',
             'issue_url': f'{api}/issues/{number}', 'user': user, 'actor': user,
             'created_at': '2026-10-23T09:40:00Z', 'updated_at': '2026-10-23T09:45:00Z',
             'author_association': 'NONE', 'body': 'Synthetic whole comment body.'},
        ]

    def transport(self, request):
        self.calls.append(copy.deepcopy(request))
        if request['endpoint'] == 'graphql':
            variables = request['body']['variables']
            raw = self.graphql[(variables['number'], variables['cursor'])]
            return {'status': 200, 'headers': {'content-type': 'application/json'}, 'body': bounded.canonical(raw)}
        if request['endpoint'] not in self.responses:
            raise ValueError('synthetic:missing-whole-original:' + request['endpoint'])
        return copy.deepcopy(self.responses[request['endpoint']])

    def release(self):
        return collector.Collector(self.m, self.transport, lambda: NOW).collect_release(self.release_selection, self.review)

    def pair(self, release):
        return collector.Collector(self.m, self.transport, lambda: NOW).collect_pair(
            self.pair_selection, self.manifest, release)

    def tick_clock(self, seconds=1):
        step = -1

        def clock():
            nonlocal step
            step += 1
            return NOW + timedelta(seconds=seconds * step)
        return clock
