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
