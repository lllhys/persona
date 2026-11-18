"""Pure unit coverage for the synthetic native UI reading fixture data."""
import importlib.util
from pathlib import Path
import unittest


FIXTURE_PATH = Path(__file__).with_name('native-ui-fixture.py')
SPEC = importlib.util.spec_from_file_location('native_ui_fixture_under_test', FIXTURE_PATH)
if SPEC is None or SPEC.loader is None:
    raise RuntimeError('cannot load native UI fixture')
fixture = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(fixture)


class ReadingFixtureTests(unittest.TestCase):
    def test_default_transcript_and_continuation_are_unchanged(self):
        expected_items = [
            {'id': 'fixture-user-1#0', 'seq': 1, 'role': 'user', 'kind': 'text',
             'text': 'Show the synthetic UI fixture overview.'},
            {'id': 'fixture-assistant-2#0', 'seq': 2, 'role': 'assistant', 'kind': 'text',
             'text': 'This conversation contains synthetic plan and receipt examples.'},
        ]
        expected = {
            'items': expected_items, 'history': 'fixture-transcript-v1', 'to_seq': 2,
            'backlog': False, 'has_more': False, 'more_after': False,
        }
        self.assertEqual(fixture.transcript(), expected)
        self.assertEqual(fixture.transcript(control={}), expected)
        self.assertEqual(fixture.transcript(control={'reading_case': 'true'}), expected)
        self.assertEqual(fixture.transcript(control={'reading_append': True}), expected)
        self.assertEqual(fixture.transcript(after_seq=1), {
            **expected, 'items': expected_items[1:], 'from_seq': 1,
        })

    def test_reading_case_has_stable_ordered_unique_wrapping_messages(self):
        response = fixture.transcript(control={'reading_case': True})
        items = response['items']
        self.assertEqual([item['seq'] for item in items], list(range(40, 60)))
        self.assertEqual([item['role'] for item in items], ['user', 'assistant'] * 10)
        self.assertEqual(len({item['id'] for item in items}), 20)
        self.assertTrue(all(len(item['text']) >= 150 for item in items))
        self.assertEqual(response['to_seq'], 59)
        expanded = fixture.transcript(control={
            'reading_case': True, 'reading_prepend': True, 'reading_append': True,
        })
        self.assertEqual(expanded['history'], response['history'])
        self.assertEqual([item['seq'] for item in expanded['items']], list(range(20, 65)))
        self.assertEqual([item['role'] for item in expanded['items']], ['user', 'assistant'] * 22 + ['user'])
        self.assertEqual(len({item['id'] for item in expanded['items']}), 45)
        self.assertEqual(expanded['items'][20:40], items)

    def test_after_seq_returns_only_new_rows_with_real_boundaries(self):
        control = {'reading_case': True}
        page = fixture.transcript(after_seq=45, control=control)
        self.assertEqual([item['seq'] for item in page['items']], list(range(46, 60)))
        self.assertEqual((page['from_seq'], page['to_seq']), (45, 59))
        self.assertEqual(page['history'], fixture.TRANSCRIPT_HISTORY)

        appended = fixture.transcript(
            after_seq=59,
            control={'reading_case': True, 'reading_append': True},
        )
        self.assertEqual([item['seq'] for item in appended['items']], list(range(60, 65)))
        self.assertEqual((appended['from_seq'], appended['to_seq']), (59, 64))
        self.assertEqual(appended['history'], page['history'])

        empty = fixture.transcript(after_seq=64, control={'reading_case': True, 'reading_append': True})
        self.assertEqual(empty['items'], [])
        self.assertEqual((empty['from_seq'], empty['to_seq']), (64, 64))
        future = fixture.transcript(after_seq=99, control={'reading_case': True, 'reading_append': True})
        self.assertEqual((future['from_seq'], future['to_seq']), (99, 99))

    def test_prepend_and_before_seq_share_the_same_history(self):
        control = {'reading_case': True, 'reading_prepend': True}
        expanded = fixture.transcript(control=control)
        self.assertEqual([item['seq'] for item in expanded['items']], list(range(20, 60)))

        older = fixture.transcript(before_seq=40, control=control)
        self.assertEqual([item['seq'] for item in older['items']], list(range(20, 40)))
        self.assertEqual((older['from_seq'], older['to_seq']), (20, 39))
        self.assertEqual(older['history'], expanded['history'])

    def test_control_flags_require_json_booleans(self):
        self.assertEqual(fixture.optional_bool({'flag': True}, 'flag'), True)
        self.assertEqual(fixture.optional_bool({'flag': False}, 'flag'), False)
        for invalid in ('true', 'false', 1, 0, None, [], {}):
            with self.subTest(invalid=invalid):
                self.assertIsNone(fixture.optional_bool({'flag': invalid}, 'flag'))

        case_with_invalid_additions = fixture.transcript(control={
            'reading_case': True, 'reading_prepend': 'true', 'reading_append': 1,
        })
        self.assertEqual([item['seq'] for item in case_with_invalid_additions['items']], list(range(40, 60)))
        invalid_case = fixture.transcript(control={
            'reading_case': 1, 'reading_prepend': True, 'reading_append': True,
        })
        self.assertEqual(len(invalid_case['items']), 2)

    def test_agent_working_override_changes_only_lead_state(self):
        baseline = fixture.session_rows()
        baseline_lead = next(row for row in baseline if row['id'] == fixture.LEAD_SESSION)
        self.assertEqual(baseline_lead['state'], 'idle')

        for override, expected in ((True, 'working'), (False, 'idle')):
            with self.subTest(override=override):
                lead = next(row for row in fixture.session_rows(agent_working=override)
                            if row['id'] == fixture.LEAD_SESSION)
                self.assertEqual(lead['state'], expected)
                self.assertEqual({key: value for key, value in lead.items() if key != 'state'},
                                 {key: value for key, value in baseline_lead.items() if key != 'state'})
                assistant = fixture.conversations(agent_working=override)
                conversation = next(row for row in assistant if row['id'] == fixture.LEAD_SESSION)
                self.assertEqual(conversation['state'], expected)
                self.assertEqual(conversation['id'], fixture.LEAD_SESSION)
                self.assertEqual(conversation['machine_id'], baseline_lead['machine_id'])
                self.assertEqual(conversation['session_id'], baseline_lead['session_id'])

        for invalid in ('true', 1, None):
            with self.subTest(invalid=invalid):
                lead = next(row for row in fixture.session_rows(agent_working=invalid)
                            if row['id'] == fixture.LEAD_SESSION)
                self.assertEqual(lead['state'], baseline_lead['state'])

        recovered = fixture.session_rows(['fixture-todo-interrupted'])
        recovered_lead = next(row for row in recovered if row['id'] == fixture.LEAD_SESSION)
        self.assertEqual(recovered_lead['state'], 'working')
        self.assertNotIn('turn_error', recovered_lead)

        recovered_idle = fixture.session_rows(['fixture-todo-interrupted'], agent_working=False)
        recovered_idle_lead = next(row for row in recovered_idle if row['id'] == fixture.LEAD_SESSION)
        self.assertEqual(recovered_idle_lead['state'], 'idle')
        self.assertNotIn('turn_error', recovered_idle_lead)
        self.assertNotIn('turn_error_kind', recovered_idle_lead)
        self.assertEqual({key: value for key, value in recovered_idle_lead.items() if key != 'state'},
                         {key: value for key, value in recovered_lead.items() if key != 'state'})


    def test_paged_reading_requires_explicit_older_fetch_and_preserves_the_live_cursor(self):
        control = {'reading_case': True, 'reading_prepend': True, 'reading_paged': True}
        latest = fixture.transcript(control=control)
        self.assertEqual([item['seq'] for item in latest['items']], list(range(40, 60)))
        self.assertTrue(latest['has_more'])
        older = fixture.transcript(before_seq=40, control=control)
        self.assertEqual([item['seq'] for item in older['items']], list(range(20, 40)))
        self.assertFalse(older['has_more'])
        self.assertEqual(older['from_seq'], 20)
        append = fixture.transcript(after_seq=59, control={**control, 'reading_append': True})
        self.assertEqual([item['seq'] for item in append['items']], list(range(60, 65)))
        self.assertEqual(append['from_seq'], 59)
        self.assertTrue(append['has_more'])

    def test_paged_after_cursor_is_the_exclusive_span_boundary(self):
        page = fixture.transcript(after_seq=40, control={
            'reading_case': True, 'reading_prepend': True, 'reading_paged': True,
        })
        self.assertEqual([item['seq'] for item in page['items']], list(range(41, 60)))
        self.assertEqual((page['from_seq'], page['to_seq']), (40, 59))

    def test_interrupted_receipt_resolution_tracks_its_own_successful_recovery(self):
        baseline = fixture.feed_items()
        pending = next(item for item in baseline if item['id'] == 'fixture-receipt-interrupted')
        self.assertEqual(pending['body']['resolution'], 'pending')
        unrelated = fixture.feed_items(['fixture-todo-failed'])
        self.assertEqual(unrelated, baseline)
        recovered = fixture.feed_items(['fixture-todo-interrupted'])
        resumed = next(item for item in recovered if item['id'] == pending['id'])
        self.assertEqual(resumed['body']['resolution'], 'resumed')
        self.assertEqual({**resumed, 'body': {**resumed['body'], 'resolution': 'pending'}}, pending)
        self.assertEqual([item for item in recovered if item['id'] != pending['id']],
                         [item for item in baseline if item['id'] != pending['id']])

if __name__ == '__main__':
    unittest.main()
