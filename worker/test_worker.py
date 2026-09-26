import unittest

from worker.worker import embed_texts


class FakeModel:
    def __init__(self):
        self.kwargs = None

    def encode(self, texts, **kwargs):
        self.kwargs = kwargs
        return [[1.0, 0.0] for _ in texts]


class WorkerTest(unittest.TestCase):
    def test_qwen_query_uses_query_prompt(self):
        model = FakeModel()
        vectors = embed_texts(model, "qwen", "query", ["waterfall"])
        self.assertEqual(vectors, [[1.0, 0.0]])
        self.assertEqual(model.kwargs["prompt_name"], "query")


if __name__ == "__main__":
    unittest.main()
