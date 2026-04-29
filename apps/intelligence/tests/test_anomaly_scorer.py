import os
import sys
import unittest

sys.path.insert(0, os.path.abspath(os.path.join(os.path.dirname(__file__), '../app/services')))
from anomaly_scorer import AnomalyScorer

class TestAnomalyScorer(unittest.TestCase):
    def setUp(self):
        self.scorer = AnomalyScorer()

    def test_amount_deviation_score(self):
        transaction = {'amount': 300}
        user_history = {'avg_withdrawal': 100}
        score = self.scorer._amount_deviation_score(transaction, user_history)
        self.assertGreaterEqual(score, 60)

    def test_velocity_score(self):
        transaction = {}
        user_history = {'recent_withdrawals': 6}
        score = self.scorer._velocity_score(transaction, user_history)
        self.assertGreaterEqual(score, 80)

    def test_settlement_account_score(self):
        transaction = {'to_account': 'abc'}
        user_history = {'known_accounts': set(['def'])}
        score = self.scorer._settlement_account_score(transaction, user_history)
        self.assertGreaterEqual(score, 70)

    def test_weighted_average(self):
        factors = [10, 20, 30]
        avg = self.scorer._weighted_average(factors)
        self.assertEqual(avg, 20)

    def test_recommend_action(self):
        self.assertEqual(self.scorer._recommend_action(20), 'allow')
        self.assertEqual(self.scorer._recommend_action(50), 'flag')
        self.assertEqual(self.scorer._recommend_action(80), 'hold')
        self.assertEqual(self.scorer._recommend_action(95), 'block')

if __name__ == '__main__':
    unittest.main()
