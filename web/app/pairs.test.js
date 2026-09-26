import test from "node:test";
import assert from "node:assert/strict";
import { addPair } from "./pairs.js";

test("addPair rejects a fifth or duplicate pair", () => {
  const four = [
    { store: "postgres", model: "minilm" },
    { store: "mongo", model: "minilm" },
    { store: "weaviate", model: "minilm" },
    { store: "pinecone", model: "minilm" },
  ];
  assert.equal(addPair(four, { store: "postgres", model: "qwen" }).length, 4);
  assert.equal(addPair(four, four[0]).length, 4);
});
