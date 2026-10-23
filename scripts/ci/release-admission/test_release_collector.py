"""Real offline collection/replay tests with complete synthetic provider bodies."""
import copy
from datetime import timedelta
import hashlib
import unittest

import bounded
import collector_test_support as support
import release_collector as c
import release_policy as policy


def install_timeline(fixture, number, pages):
    for i, rows in enumerate(pages, 1):
        endpoint = f'{c.REPO}/pulls/{number}/timeline?per_page=100&page={i}'
        headers = ({'link': f'<https://api.github.com/{endpoint.rsplit("=", 1)[0]}={i + 1}>; rel="next"'}
                   if i < len(pages) else {})
        fixture.put(endpoint, rows, headers)


def replace_timeline_originals(packet, number, pages):
    """Supply changed external bodies with coherent integrity/projection metadata.

    No validator is patched. Both first reads and final rereads change together;
    positive controls prove the resulting complete envelope can replay.
    """
    changed = copy.deepcopy(packet)
    prefix = f'{c.REPO}/pulls/{number}/timeline?per_page=100&page='
    for original in changed['originals']:
        endpoint = original['request']['endpoint']
        if endpoint.startswith(prefix):
            rows = pages[int(endpoint[len(prefix):]) - 1]
            raw = bounded.canonical(rows)
            original.update(body=raw.decode(), bodyBytes=len(raw),
                            bodySHA256=hashlib.sha256(raw).hexdigest(),
                            semanticSHA256=hashlib.sha256(raw).hexdigest())
    episode = 'episode' if packet['schema'] == c.RELEASE_SCHEMA else 'mainEpisode'
    changed['code'][episode]['timelinePages'] = copy.deepcopy(pages)
    for readback in changed['code']['finalReadbacks']:
        readback['semanticSHA256'] = changed['originals'][readback['captureIndex']]['semanticSHA256']
    changed['rawResponseBytes'] = sum(row['bodyBytes'] for row in changed['originals'])
    return changed


class CollectorTests(unittest.TestCase):
    def setUp(self):
        self.f = support.Fixture()

    def assert_refused(self, action, label=None):
        with self.assertRaisesRegex(ValueError, label or '.'):
            action()

    def timeline_context(self, lane, *, multiple_pages=False):
        fixture = support.Fixture()
        number = fixture.release_pr if lane == 'release' else fixture.main_pr
        endpoint = f'{c.REPO}/pulls/{number}/timeline?per_page=100&page=1'
        earlier, merged = fixture.read_value(endpoint)
        native = fixture.timeline_variants(number)
        if multiple_pages:
            # Exactly 100 distinct entries on the nonterminal first page.
            first = native + [dict(earlier, id=i + 1, node_id=f'earlier-{i}') for i in range(97)]
            pages = [first, [merged]]
        else:
            pages = [native + [earlier, merged]]
        install_timeline(fixture, number, pages)
        release = fixture.release()
        if lane == 'release':
            packet, live = release, fixture.release
            replay = lambda value: c.release_checked(fixture.m, value)
        else:
            packet, live = fixture.pair(release), lambda: fixture.pair(release)
            replay = lambda value: c.pair_checked(fixture.m, value, fixture.manifest, release, support.NOW)
        replay(packet)
        return fixture, number, pages, packet, live, replay

    def check_timeline_refusal(self, context, pages, label):
        fixture, number, _, packet, live, replay = context
        install_timeline(fixture, number, pages)
        with self.subTest(path='live-collection'):
            self.assert_refused(live, label)
        changed = replace_timeline_originals(packet, number, pages)
        with self.subTest(path='whole-original-replay'):
            self.assert_refused(lambda: replay(changed), label)

    def test_r2_2_counterexamples_refuse_release_and_main_live_and_replay(self):
        variants = {
            'unknown-semantic': [{'event': 'release_approval_superseded', 'id': 999999,
                                  'created_at': '2026-10-23T09:00:00Z'}],
            'unknown-identityless': [{'event': 'future_event'}],
            'review-identityless': [{'event': 'reviewed'}],
            'duplicate-review-identityless': [{'event': 'reviewed'}, {'event': 'reviewed'}],
        }
        for lane in ('release', 'main'):
            context = self.timeline_context(lane)
            for name, rows in variants.items():
                with self.subTest(lane=lane, case=name):
                    pages = copy.deepcopy(context[2])
                    pages[0] = copy.deepcopy(rows) + pages[0]
                    self.check_timeline_refusal(context, pages, 'unsupported-timeline-event|positive-exact-provider-id')

    def test_unqualified_variants_and_malformed_rows_refuse_both_lanes_and_replay(self):
        variants = [None, True, [], 'reviewed', {}, {'event': None}, {'event': True}]
        for event in ('cross-referenced', 'line-commented', 'commit-commented', 'future_event'):
            variants.append({'event': event, 'id': 999999, 'node_id': 'unqualified-native-node',
                             'created_at': '2026-10-23T09:00:00Z'})
        for lane in ('release', 'main'):
            context = self.timeline_context(lane)
            for row in variants:
                with self.subTest(lane=lane, row=row):
                    pages = copy.deepcopy(context[2])
                    pages[0].insert(0, row)
                    self.check_timeline_refusal(context, pages, 'unsupported-timeline-event')

    def test_native_timeline_shapes_preserve_whole_rows_order_and_replay(self):
        for lane in ('release', 'main'):
            with self.subTest(lane=lane):
                fixture, number, pages, packet, live, replay = self.timeline_context(lane)
                self.assertEqual(len({row['id'] for row in pages[0][1:3]}), 1)
                for row in pages[0][:3]:
                    self.assertNotIn('created_at' if row['event'] != 'commented' else 'submitted_at', row)
                self.assertNotIn('id', pages[0][0])
                before = copy.deepcopy(packet)
                endpoint = f'{c.REPO}/pulls/{number}/timeline?per_page=100&page=1'
                captures = [row for row in packet['originals'] if row['request']['endpoint'] == endpoint]
                self.assertEqual(len(captures), 2)
                self.assertTrue(all(row['body'].encode() == fixture.responses[endpoint]['body'] for row in captures))
                # A valid changed whole external comment remains replayable after
                # updating only its integrity/projection metadata.
                pages[0][2]['body'] += ' Preserved additional text.'
                install_timeline(fixture, number, pages)
                produced = live()
                rewritten = replace_timeline_originals(packet, number, pages)
                self.assertEqual(produced, rewritten)
                replay(rewritten)
                self.assertEqual(packet, before)
                self.assertTrue(all(produced[key] is False for key in c.FLAGS))

    def test_missing_and_wrong_native_identity_refuse_both_lanes_and_replay(self):
        for lane in ('release', 'main'):
            context = self.timeline_context(lane)
            for position, row in enumerate(context[2][0][:-1]):
                field = 'sha' if row['event'] == 'committed' else 'id'
                bad_values = (None, True, 0, -1, '123', 1.5)
                if field == 'sha':
                    bad_values += ('a' * 39, 'g' * 40, 'A' * 40)
                for value in bad_values:
                    with self.subTest(lane=lane, event=row['event'], field=field, value=value):
                        pages = copy.deepcopy(context[2])
                        if value is None:
                            del pages[0][position][field]
                        else:
                            pages[0][position][field] = value
                        if field == 'sha':
                            pages[0][position]['id'] = 7654321
                        self.check_timeline_refusal(context, pages, 'timeline-commit-identity|positive-exact-provider-id')
                for value in (None, True, '', 'n' * 201):
                    with self.subTest(lane=lane, event=row['event'], field='node_id', value=value):
                        pages = copy.deepcopy(context[2])
                        if value is None:
                            del pages[0][position]['node_id']
                        else:
                            pages[0][position]['node_id'] = value
                        self.check_timeline_refusal(context, pages, 'timeline-native-node-id')
            for value in (None, True, '123'):
                with self.subTest(lane=lane, event='committed', extra_id=value):
                    pages = copy.deepcopy(context[2])
                    pages[0][0]['id'] = value
                    self.check_timeline_refusal(context, pages, 'positive-exact-provider-id')

    def test_native_timeline_times_and_sha_fields_refuse_both_lanes_and_replay(self):
        for lane in ('release', 'main'):
            context = self.timeline_context(lane)
            paths = ((0, ('author', 'date')), (0, ('committer', 'date')),
                     (1, ('submitted_at',)), (2, ('created_at',)), (3, ('created_at',)))
            for position, path in paths:
                for value in (None, True, 42, '', '2026-02-30T09:00:00Z',
                              '2026-10-23T09:00:00', '2026-10-23T12:00:01Z'):
                    with self.subTest(lane=lane, position=position, path=path, value=value):
                        pages = copy.deepcopy(context[2])
                        target = pages[0][position]
                        # A top-level timestamp cannot supply a missing native
                        # commit or review timestamp.
                        if position in (0, 1):
                            target['created_at'] = '2026-10-23T09:00:00Z'
                        for part in path[:-1]:
                            target = target[part]
                        if value is None:
                            del target[path[-1]]
                        else:
                            target[path[-1]] = value
                        self.check_timeline_refusal(context, pages, 'timeline-native-time|real-timeline-time|explicit-utc-time|day is out of range')
            for position in (0, 1):
                for value in ('2026-10-23T09:00:00+00:60', '2026-10-23T09:00:00+24:00'):
                    with self.subTest(lane=lane, position=position, value=value):
                        pages = copy.deepcopy(context[2])
                        if position == 0:
                            pages[0][position]['committer']['date'] = value
                        else:
                            pages[0][position]['submitted_at'] = value
                        self.check_timeline_refusal(context, pages, 'timeline-native-time|explicit-utc-time')
            for position in (0, 1, 3):
                field = 'sha' if position == 0 else 'commit_id'
                for value in (True, 'a' * 39, 'g' * 40):
                    with self.subTest(lane=lane, position=position, field=field, value=value):
                        pages = copy.deepcopy(context[2])
                        pages[0][position][field] = value
                        self.check_timeline_refusal(context, pages, 'timeline-commit-identity|timeline-event-commit-sha')
            for role in ('author', 'committer'):
                for value in (None, [], 'synthetic-person', {}):
                    with self.subTest(lane=lane, role=role, value=value):
                        pages = copy.deepcopy(context[2])
                        if value is None:
                            del pages[0][0][role]
                        else:
                            pages[0][0][role] = value
                        self.check_timeline_refusal(context, pages, 'timeline-native-time')
            for value in (None, False, 'a' * 41):
                with self.subTest(lane=lane, event='reviewed', commit_id=value):
                    pages = copy.deepcopy(context[2])
                    if value is None:
                        del pages[0][1]['commit_id']
                    else:
                        pages[0][1]['commit_id'] = value
                    self.check_timeline_refusal(context, pages, 'timeline-event-commit-sha')

    def test_duplicate_native_identities_across_pages_and_event_names_refuse(self):
        for lane in ('release', 'main'):
            context = self.timeline_context(lane, multiple_pages=True)
            for location in ('same-page', 'next-page'):
                for position in range(4):
                    for key in ('native-id', 'node_id'):
                        with self.subTest(lane=lane, location=location, position=position, duplicate=key):
                            pages = copy.deepcopy(context[2])
                            duplicate = copy.deepcopy(pages[0][position])
                            if key == 'node_id':
                                if position == 0:
                                    duplicate['sha'] = support.sha('different-native-commit')
                                else:
                                    duplicate['id'] = 999999
                            else:
                                duplicate['node_id'] = 'different-native-node'
                                if position == 0:
                                    duplicate['id'] = 999999
                                elif position == 3:
                                    # Same issue-event ID, different supported
                                    # event name and node: no new ID namespace.
                                    duplicate['event'] = 'mentioned'
                            if location == 'same-page':
                                pages[0][-1] = duplicate
                            else:
                                pages[1].insert(0, duplicate)
                            self.check_timeline_refusal(context, pages, 'duplicate-timeline-native-identity')

    def test_two_releases_and_pairs_use_identical_implementation(self):
        observed = []
        for version in (1, 2):
            with self.subTest(version=version):
                fixture = support.Fixture(version)
                release = fixture.release()
                pair = fixture.pair(release)
                self.assertEqual(c.pair_checked(fixture.m, pair, fixture.manifest, release, support.NOW),
                                 {'candidate': fixture.candidate_sha, 'workflow': fixture.main_sha})
                self.assertEqual(len(pair['code']['ciRuns']['candidate']['jobPages'][0]['jobs']), 49)
                self.assertEqual(len(pair['code']['ciRuns']['workflow']['jobPages'][0]['jobs']), 48)
                self.assertNotEqual(pair['code']['ciRuns']['workflow']['id'], release['code']['episode']['queueRun']['id'])
                self.assertEqual(set(pair['code']['candidateFiles']), {'go.mod'})
                self.assertEqual(set(pair['code']['candidateCIFiles']), policy.CANDIDATE_CI_FILES)
                self.assertTrue(all(pair[key] is False and release[key] is False for key in c.FLAGS))
                self.assertTrue(pair['projectionPaths'])
                self.assertTrue(all(row['bodyBytes'] == len(row['body'].encode()) for row in pair['originals']))
                self.assertTrue(all(request['method'] == ('POST' if request['endpoint'] == 'graphql' else 'GET')
                                    for request in fixture.calls))
                observed.append(c.collection_digest(fixture.m, release))
        self.assertNotEqual(*observed)

    def test_main_equal_release_can_share_explicit_queue_selection(self):
        fixture = support.Fixture(descendant=False)
        release = fixture.release()
        pair = fixture.pair(release)
        self.assertEqual(c.pair_checked(fixture.m, pair, fixture.manifest, release, support.NOW)['workflow'], fixture.release_sha)
        self.assertEqual(pair['code']['releaseLineage']['commits'], [])

    def test_changed_source_cannot_reuse_old_release(self):
        release = self.f.release()
        endpoint = c.REPO + '/commits/' + self.f.main_sha
        current = self.f.read_value(endpoint)
        current['commit']['tree']['sha'] = self.f.tree(support.file_bytes(2))
        self.f.put(endpoint, current)
        self.assert_refused(lambda: self.f.pair(release), 'main-covered-by-release-vector')

    def test_wrong_same_tree_public_sha_is_not_queue_correspondence(self):
        endpoint = f'{c.REPO}/actions/runs/{self.f.release_run}/attempts/1'
        row = self.f.read_value(endpoint)
        row['head_sha'] = self.f.release_head
        self.f.put(endpoint, row)
        self.f.put(f'{c.REPO}/actions/runs/{self.f.release_run}', row)
        self.assert_refused(self.f.release, 'exact-complete-ci-role')

    def test_reversed_public_parents_refused(self):
        endpoint = c.REPO + '/commits/' + self.f.release_sha
        row = self.f.read_value(endpoint)
        row['parents'].reverse()
        self.f.put(endpoint, row)
        self.assert_refused(self.f.release, 'public-ordered-merge-parents')

    def test_missing_timeline_next_page_refused(self):
        endpoint = f'{c.REPO}/pulls/{self.f.release_pr}/timeline?per_page=100&page=1'
        rows = self.f.read_value(endpoint)
        rows = [dict(rows[0], id=i + 1, node_id=f'earlier-{i}') for i in range(99)] + [rows[1]]
        self.f.put(endpoint, rows, {'link': f'<https://api.github.com/{endpoint[:-1]}2>; rel="next"'})
        self.assert_refused(self.f.release, 'missing-whole-original')

    def test_missing_terminal_timeline_header_refused(self):
        endpoint = f'{c.REPO}/pulls/{self.f.release_pr}/timeline?per_page=100&page=1'
        self.f.responses[endpoint]['headers']['link'] = f'<https://api.github.com/{endpoint[:-1]}2>; rel="last"'
        # A terminal page cannot claim a later last page.
        self.assert_refused(self.f.release)

    def test_final_timeline_pagination_growth_refuses_live_and_replay(self):
        endpoint = f'{c.REPO}/pulls/{self.f.release_pr}/timeline?per_page=100&page=1'
        earlier, merged = self.f.read_value(endpoint)
        self.f.put(endpoint, [dict(earlier, id=i + 1, node_id=f'earlier-{i}') for i in range(99)] + [merged])
        next_endpoint = endpoint[:-1] + '2'
        self.f.put(next_endpoint, [dict(earlier, id=999999, node_id='later-native-event')])
        next_link = f'<https://api.github.com/{next_endpoint}>; rel="next"'
        calls = []
        def growing(request):
            response = self.f.transport(request)
            if request['endpoint'] == endpoint:
                calls.append(True)
                if len(calls) == 2:
                    response['headers']['link'] = next_link
            return response
        self.assert_refused(lambda: c.Collector(self.f.m, growing, lambda: support.NOW)
                            .collect_release(self.f.release_selection, self.f.review), 'pagination')
        self.assertFalse(any(row['endpoint'] == next_endpoint for row in self.f.calls))
        valid = self.f.release()
        final = next(row for row in reversed(valid['originals']) if row['request']['endpoint'] == endpoint)
        final['headers']['link'] = next_link
        self.assert_refused(lambda: c.release_checked(self.f.m, valid), 'pagination')

    def test_final_pagination_compares_links_without_volatile_headers_or_order(self):
        endpoint = f'{c.REPO}/pulls/{self.f.release_pr}/timeline?per_page=100&page=1'
        earlier, merged = self.f.read_value(endpoint)
        next_endpoint = endpoint[:-1] + '2'
        links = [f'<https://api.github.com/{next_endpoint}>; rel="next"',
                 f'<https://api.github.com/{next_endpoint}>; rel="last"']
        self.f.put(endpoint, [dict(earlier, id=i + 1, node_id=f'earlier-{i}') for i in range(100)],
                   {'link': ', '.join(links)})
        self.f.put(next_endpoint, [merged])
        calls = []
        def reordered(request):
            response = self.f.transport(request)
            if request['endpoint'] == endpoint:
                calls.append(True)
                if len(calls) == 2:
                    response['headers'].update(link=', '.join(reversed(links)),
                        date='Fri, 23 Oct 2026 10:00:00 GMT', **{'x-github-request-id': 'fresh-request'})
            return response
        proof = c.Collector(self.f.m, reordered, lambda: support.NOW).collect_release(self.f.release_selection, self.f.review)
        self.assertEqual(c.release_checked(self.f.m, proof), proof['code'])

    def test_complete_two_page_timeline_retains_earlier_episodes(self):
        endpoint = f'{c.REPO}/pulls/{self.f.release_pr}/timeline?per_page=100&page=1'
        earlier, merged = self.f.read_value(endpoint)
        rows = [dict(earlier, id=i + 1, node_id=f'earlier-{i}') for i in range(100)]
        next_endpoint = endpoint[:-1] + '2'
        self.f.put(endpoint, rows, {'link': f'<https://api.github.com/{next_endpoint}>; rel="next"'})
        self.f.put(next_endpoint, [merged])
        release = self.f.release()
        self.assertEqual([len(rows) for rows in release['code']['episode']['timelinePages']], [100, 1])
        self.assertEqual(c.release_checked(self.f.m, release)['episode']['pullRequest']['mergeSHA'], self.f.release_sha)

    def test_complete_multi_page_source_range_and_missing_page(self):
        head, commits = self.f.anchor, []
        for i in range(101):
            head = self.f.commit(f'long-range-{i}', [head], self.f.files)
            commits.append(head)
        merge = self.f.commit('long-range-merge', [self.f.anchor, head], self.f.files)
        self.f.release_head, self.f.release_sha = head, merge
        self.f.release_selection['mergeSHA'] = merge
        self.f.episode(self.f.release_pr, head, self.f.anchor, merge, self.f.release_run)
        pr_endpoint = f'{c.REPO}/pulls/{self.f.release_pr}'
        row = self.f.read_value(pr_endpoint)
        row['commits'] = 101
        self.f.put(pr_endpoint, row)
        page1 = pr_endpoint + '/commits?per_page=100&page=1'
        page2 = page1[:-1] + '2'
        self.f.put(page1, [self.f.read_value(c.REPO + '/commits/' + oid) for oid in commits[:100]],
                   {'link': f'<https://api.github.com/{page2}>; rel="next"'})
        self.assert_refused(self.f.release, 'missing-whole-original')
        self.f.put(page2, [self.f.read_value(c.REPO + '/commits/' + commits[-1])])
        release = self.f.release()
        self.assertEqual(release['code']['episode']['sourceRange']['total_count'], 101)

    def test_missing_job_or_extra_job_refused(self):
        endpoint = f'{c.REPO}/actions/runs/{self.f.release_run}/attempts/1/jobs?per_page=100&page=1'
        original = self.f.read_value(endpoint)
        for kind in ('missing', 'extra', 'duplicate', 'failed', 'skipped'):
            with self.subTest(kind=kind):
                body = copy.deepcopy(original)
                if kind == 'missing':
                    body['jobs'].pop()
                    body['total_count'] -= 1
                elif kind == 'extra':
                    body['jobs'].append(dict(body['jobs'][0], name='invented', id=999999))
                    body['total_count'] += 1
                elif kind == 'duplicate':
                    body['jobs'][-1] = body['jobs'][0]
                else:
                    body['jobs'][0]['conclusion'] = 'failure' if kind == 'failed' else 'skipped'
                self.f.put(endpoint, body)
                self.assert_refused(self.f.release)

    def test_source_signature_and_author_matching_dco_required(self):
        endpoint = c.REPO + '/commits/' + self.f.release_head
        original = self.f.read_value(endpoint)
        for field in ('signature', 'dco'):
            with self.subTest(field=field):
                row = copy.deepcopy(original)
                if field == 'signature':
                    row['commit']['verification']['verified'] = False
                else:
                    row['commit']['message'] = 'Signed-off-by: Someone Else <else@example.invalid>'
                self.f.put(endpoint, row)
                self.f.put(f'{c.REPO}/pulls/{self.f.release_pr}/commits?per_page=100&page=1', [row])
                self.assert_refused(self.f.release)

    def test_wrong_owner_and_graphql_tuple_refused(self):
        key = (self.f.release_pr, None)
        original = copy.deepcopy(self.f.graphql[key])
        for field in ('owner', 'tuple', 'event', 'errors', 'pagination'):
            with self.subTest(field=field):
                row = copy.deepcopy(original)
                node = row['data']['repository']['pullRequest']
                if field == 'owner':
                    node['timelineItems']['nodes'][0]['actor']['databaseId'] = 42
                elif field == 'tuple':
                    node['headRefOid'] = self.f.anchor
                elif field == 'event':
                    node['timelineItems']['nodes'][0]['id'] = 'other-event'
                elif field == 'errors':
                    row['errors'] = [{'message': 'partial result'}]
                else:
                    node['timelineItems']['pageInfo'] = {'hasNextPage': True, 'endCursor': 'cursor'}
                self.f.graphql[key] = row
                self.assert_refused(self.f.release)

    def test_candidate_ci_substitution_at_head_or_tested_merge_refused(self):
        release = self.f.release()
        for role in ('head', 'tested'):
            with self.subTest(role=role):
                fixture = support.Fixture()
                files = copy.deepcopy(fixture.files)
                files['go.mod'] += '// separate candidate dependency vector\n'
                files['scripts/ci/pr-gate/main.go'] = 'substitute gate\n'
                oid = fixture.candidate_sha if role == 'head' else fixture.candidate_merge
                endpoint = c.REPO + '/commits/' + oid
                raw = fixture.read_value(endpoint)
                raw['commit']['tree']['sha'] = fixture.tree(files)
                fixture.put(endpoint, raw)
                self.assert_refused(lambda: fixture.pair(release))

    def test_stale_main_proof_or_borrowed_anchor_run_refused(self):
        release = self.f.release()
        selection = copy.deepcopy(self.f.pair_selection)
        selection['mainRunId'] = self.f.release_run
        self.assert_refused(lambda: c.Collector(self.f.m, self.f.transport, lambda: support.NOW).collect_pair(
            selection, self.f.manifest, release), 'exact-complete-ci-role')
        pair = self.f.pair(release)
        self.assert_refused(lambda: c.pair_checked(self.f.m, pair, self.f.manifest, release,
                                                support.NOW + timedelta(seconds=301)), 'fresh-complete')

    def test_mutable_pointer_and_new_run_attempt_drift_refused(self):
        release = self.f.release()
        for endpoint in (c.REPO + '/git/ref/heads/main', f'{c.REPO}/actions/runs/{self.f.candidate_run}'):
            with self.subTest(endpoint=endpoint):
                count = 0

                def drifting(request):
                    nonlocal count
                    response = self.f.transport(request)
                    if request['endpoint'] == endpoint:
                        count += 1
                        if count == 2:
                            body = bounded.strict_json(response['body'])
                            if endpoint.endswith('main'):
                                body['object']['sha'] = self.f.release_sha
                            else:
                                body['run_attempt'] = 2
                            response['body'] = bounded.canonical(body)
                    return response

                self.assert_refused(lambda: c.Collector(self.f.m, drifting, lambda: support.NOW).collect_pair(
                    self.f.pair_selection, self.f.manifest, release), 'stable-whole')

    def test_forged_raw_body_projection_request_and_false_authority_flags_refused(self):
        release = self.f.release()
        pair = self.f.pair(release)
        for field in ('body', 'body-hash', 'projection', 'request', 'code', 'flag', 'selection', 'missing-original'):
            with self.subTest(field=field):
                changed = copy.deepcopy(pair)
                if field == 'body':
                    changed['originals'][0]['body'] += ' '
                elif field == 'body-hash':
                    changed['originals'][0]['bodySHA256'] = '0' * 64
                elif field == 'projection':
                    changed['projectionPaths'][0]['captureIndex'] = 99
                elif field == 'request':
                    changed['originals'][0]['request']['endpoint'] = 'repos/another/project'
                elif field == 'code':
                    changed['code']['candidateFiles']['go.mod'] += 'forged\n'
                elif field == 'flag':
                    changed['runtimeReady'] = True
                elif field == 'selection':
                    changed['selection']['mainRunId'] = True
                else:
                    changed['originals'].pop()
                self.assert_refused(lambda: c.pair_checked(self.f.m, changed, self.f.manifest, release, support.NOW))

    def test_change_review_requires_whole_originals_and_complete_diff(self):
        for field in ('original', 'hash', 'path', 'baseline'):
            with self.subTest(field=field):
                fixture = support.Fixture()
                if field == 'original':
                    del fixture.review['originals']['review-original']['body']
                elif field == 'hash':
                    fixture.review['originals']['review-original']['bodySHA256'] = '0' * 64
                elif field == 'path':
                    fixture.review['changedPaths'].pop()
                else:
                    fixture.review['previousFileSHA256s'] = fixture.review['currentFileSHA256s']
                    fixture.review['changedPaths'] = []
                    fixture.review['reviews'] = {}
                self.assert_refused(fixture.release)

    def test_go_work_link_blob_and_go_version_refused(self):
        for kind in ('work', 'link', 'version', 'blob-bytes'):
            with self.subTest(kind=kind):
                fixture = support.Fixture()
                commit = fixture.read_value(c.REPO + '/commits/' + fixture.release_sha)
                tree_endpoint = c.REPO + '/git/trees/' + commit['commit']['tree']['sha']
                tree = fixture.read_value(tree_endpoint)
                if kind == 'work':
                    tree['tree'].append({'path': 'go.work', 'mode': '100644', 'type': 'blob', 'sha': support.sha('work')})
                    fixture.put(tree_endpoint, tree)
                else:
                    row = next(row for row in tree['tree'] if row['path'] == 'go.mod')
                    if kind == 'link':
                        row['mode'] = '120000'
                        fixture.put(tree_endpoint, tree)
                    elif kind == 'version':
                        files = copy.deepcopy(fixture.files)
                        files['go.mod'] = 'module synthetic\ngo 1.27.4\n'
                        commit['commit']['tree']['sha'] = fixture.tree(files)
                        fixture.put(c.REPO + '/commits/' + fixture.release_sha, commit)
                    else:
                        endpoint = c.REPO + '/git/blobs/' + row['sha']
                        blob = fixture.read_value(endpoint)
                        blob['content'] = 'Zm9yZ2Vk'
                        fixture.put(endpoint, blob)
                self.assert_refused(fixture.release)

    def test_request_time_byte_and_decoder_bounds_remain_closed(self):
        collector = c.Collector(self.f.m, self.f.transport, self.f.tick_clock(seconds=10))
        self.assert_refused(lambda: collector.collect_release(self.f.release_selection, self.f.review), 'five-minutes')
        collector = c.Collector(self.f.m, self.f.transport, lambda: support.NOW)
        collector._start()
        collector.total_bytes = policy.COLLECTION_RAW_BYTES
        self.assert_refused(lambda: collector._read(c.REPO), 'byte-budget')
        collector.total_bytes = 0
        collector.originals = [None] * policy.COLLECTION_REQUESTS
        self.assert_refused(lambda: collector._read(c.REPO), 'request-budget')
        for raw in (b'{"x":1,"x":2}', b'{"x":NaN}', b'[' * 42 + b'0' + b']' * 42,
                    b'[' + b'0,' * 2048 + b'0]', b' ' * 2_000_001):
            with self.subTest(raw_size=len(raw)):
                fixture = support.Fixture()
                fixture.responses[c.REPO]['body'] = raw
                self.assert_refused(fixture.release)

    def test_default_external_original_seam_refuses_and_never_changes_flags(self):
        release = self.f.release()
        self.assert_refused(lambda: c.require_external_originals(release, c.ExternalOriginals()), 'missing-external')
        self.assert_refused(lambda: c.require_external_originals(release, True), 'verifier-required')

        class SyntheticExternal(c.ExternalOriginals):
            def verify(self, collection):
                self.assertion = collection['originals'][0]['body']
                return True

        external = SyntheticExternal()
        c.require_external_originals(release, external)
        self.assertTrue(external.assertion)
        self.assertFalse(release['externalAuthenticityVerifiedByCollector'])

    def test_historical_anchor_cannot_be_relabelled_as_current_release(self):
        self.f.release_selection['historicalAnchorSHA'] = self.f.release_sha
        self.assert_refused(self.f.release, 'literal-historical-1017-anchor')

    def test_inner_g_code_adapter_binds_to_whole_collection(self):
        release = self.f.release()
        pair = self.f.pair(release)
        self.assertEqual(c.pair_code_checked(self.f.m, pair['code'], self.f.manifest,
                                            release, pair, support.NOW)['candidate'], self.f.candidate_sha)
        code = copy.deepcopy(pair['code'])
        code['candidateCommit']['sha'] = self.f.release_sha
        self.assert_refused(lambda: c.pair_code_checked(self.f.m, code, self.f.manifest,
                                                      release, pair, support.NOW), 'code-packet')

    def test_candidate_test_merge_need_not_be_a_signed_public_commit(self):
        release = self.f.release()
        endpoint = c.REPO + '/commits/' + self.f.candidate_merge
        row = self.f.read_value(endpoint)
        row['commit']['verification'] = {'verified': False, 'reason': 'unsigned'}
        self.f.put(endpoint, row)
        pair = self.f.pair(release)
        self.assertEqual(c.pair_checked(self.f.m, pair, self.f.manifest, release,
                                       support.NOW)['candidate'], self.f.candidate_sha)

    def test_complete_pr_range_cannot_omit_interior_source_commit(self):
        middle = self.f.commit('missing-interior', [self.f.anchor], self.f.files)
        endpoint = c.REPO + '/commits/' + self.f.release_head
        head = self.f.read_value(endpoint)
        head['parents'] = [{'sha': middle}]
        self.f.put(endpoint, head)
        self.f.put(f'{c.REPO}/pulls/{self.f.release_pr}/commits?per_page=100&page=1', [head])
        self.assert_refused(self.f.release, 'complete-topological-source-range')

    def test_first_parent_limit_and_cycles_refuse(self):
        reader = c.Collector(self.f.m, self.f.transport, lambda: support.NOW)
        reader._start()
        cursor = self.f.anchor
        for i in range(policy.FIRST_PARENT_COMMITS + 1):
            cursor = self.f.commit(f'lineage-limit-{i}', [cursor], self.f.files)
        tip = reader._commit(cursor, ('tip',))
        self.assert_refused(lambda: reader._lineage(tip, self.f.anchor, ('lineage',)), 'bounded-signed')
        endpoint = c.REPO + '/commits/' + self.f.main_sha
        row = self.f.read_value(endpoint)
        row['parents'][0]['sha'] = self.f.main_sha
        self.f.put(endpoint, row)
        release = self.f.release()
        self.assert_refused(lambda: self.f.pair(release), 'public-ordered-merge-parents')

    def test_endpoint_write_and_second_collection_refused(self):
        reader = c.Collector(self.f.m, self.f.transport, lambda: support.NOW)
        release = reader.collect_release(self.f.release_selection, self.f.review)
        self.assert_refused(lambda: reader.collect_release(self.f.release_selection, self.f.review), 'single-collection')
        self.assert_refused(lambda: reader._read('repos/other/project'), 'closed-read')
        self.assert_refused(lambda: reader._read('graphql', body={'query': 'mutation {destroy}', 'variables': {}}))
        self.assertTrue(release['originals'])


if __name__ == '__main__':
    unittest.main()
