import unittest
from router_soak import summarize


class RouterSoakSummaryTest(unittest.TestCase):
    def test_unknown_and_disabled_are_not_counted_as_enabled_failures(self):
        samples = [{'active': True, 'panel_rss_kib': 100}, {'active': False}, {},
                   {'active': False, 'disabled': True}, {'active': False}, {'active': False, 'panel_rss_kib': 130}]
        report = summarize(samples, 15)
        self.assertEqual(report['unconfirmed_enabled_samples'], 3)
        self.assertEqual(report['unknown_samples'], 1)
        self.assertEqual(report['longest_unconfirmed_sample_sequence_seconds'], 30)
        self.assertEqual(report['panel_rss_change_kib'], 30)
        self.assertIsNone(report['peak_xray_rss_kib'])

    def test_markers_skip_unknown_and_show_reboot_and_failover(self):
        samples = [
            {'elapsed_seconds': 0, 'boot_seconds': 200, 'active_profile': 'primary'},
            {'elapsed_seconds': 15},
            {'elapsed_seconds': 30, 'boot_seconds': 230, 'active_profile': 'backup'},
            {'elapsed_seconds': 45},
            {'elapsed_seconds': 60, 'boot_seconds': 7, 'active_profile': 'backup'},
        ]
        self.assertEqual(summarize(samples, 15)['transitions'], [
            {'elapsed_seconds': 30, 'kind': 'profile_changed', 'profile': 'backup'},
            {'elapsed_seconds': 60, 'kind': 'router_reboot_observed'},
        ])
