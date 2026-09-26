import { useEffect, useState } from "react";
import { Form, Link, redirect, useActionData, useFetcher, useRevalidator } from "react-router";
import { api } from "../api.server.js";
import { addPair } from "../pairs.js";

const stores = [
  ["postgres", "PostgreSQL + pgvector"],
  ["mongo", "MongoDB Atlas local"],
  ["weaviate", "Weaviate"],
  ["pinecone", "Pinecone"],
];
const models = [
  ["minilm", "Sentence-Transformers"],
  ["qwen", "Qwen local"],
  ["openai", "OpenAI"],
];
const label = (items, value) => items.find(([key]) => key === value)?.[1] || value;

export async function loader({ request }) {
  const url = new URL(request.url);
  try {
    const [corpora, runs, providers] = await Promise.all([
      api("/api/corpora"),
      api("/api/runs"),
      api("/api/providers"),
    ]);
    const selectedRun = runs.find((run) => run.id === url.searchParams.get("run")) || null;
    return { corpora, runs, providers, selectedRun, selectedCorpusID: url.searchParams.get("corpus"), apiError: null };
  } catch (error) {
    return { corpora: [], runs: [], providers: { stores: {}, models: {} }, selectedRun: null, selectedCorpusID: null, apiError: error.message };
  }
}

export async function action({ request }) {
  const form = await request.formData();
  const intent = String(form.get("intent") || "");
  try {
    if (intent === "sample") {
      const corpus = await api("/api/corpora/sample", { method: "POST" });
      return redirect(`/?corpus=${corpus.id}`);
    }
    if (intent === "upload") {
      const data = new FormData();
      data.set("name", String(form.get("name") || "Uploaded corpus"));
      for (const file of form.getAll("files")) data.append("files", file);
      const corpus = await api("/api/corpora", { method: "POST", body: data });
      return redirect(`/?corpus=${corpus.id}`);
    }
    if (intent === "run") {
      const pairs = JSON.parse(String(form.get("pairs")));
      const run = await api("/api/runs", { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify({ corpus_id: form.get("corpus_id"), pairs }) });
      return redirect(`/?run=${run.id}`);
    }
    if (intent === "search") {
      const result = await api(`/api/runs/${form.get("run_id")}/search`, { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify({ query: form.get("query") }) });
      return { kind: "search", result };
    }
    if (intent === "evaluate") {
      const evaluation = await api(`/api/runs/${form.get("run_id")}/evaluate`, { method: "POST" });
      return { kind: "evaluate", evaluation };
    }
    return { error: "Unknown action" };
  } catch (error) {
    return { error: error.message };
  }
}

function ProviderStatus({ providers }) {
  return (
    <div className="provider-list">
      {stores.map(([key, name]) => (
        <div className="provider-row" key={key} title={providers.stores[key]?.error || "Ready"}>
          <span className={providers.stores[key]?.ready ? "dot ready" : "dot"} />
          <span>{name}</span>
          <small>{providers.stores[key]?.ready ? "Ready" : "Offline"}</small>
        </div>
      ))}
    </div>
  );
}

function PairPicker({ pairs, onChange, providers }) {
  const [store, setStore] = useState("postgres");
  const [model, setModel] = useState("minilm");
  return (
    <div>
      <div className="pair-list">
        {pairs.map((pair, index) => (
          <div className="pair-chip" key={`${pair.store}-${pair.model}`}>
            <span className="pair-number">{index + 1}</span>
            <span><strong>{label(stores, pair.store)}</strong><small>{label(models, pair.model)}</small></span>
            <button type="button" aria-label={`Remove ${pair.store} with ${pair.model}`} onClick={() => onChange(pairs.filter((item) => item !== pair))}>×</button>
          </div>
        ))}
      </div>
      <div className="pair-controls">
        <label>Storage engine<select value={store} onChange={(event) => setStore(event.target.value)}>{stores.map(([key, name]) => <option key={key} value={key}>{name}{providers.stores[key]?.ready ? "" : " (offline)"}</option>)}</select></label>
        <label>Embedding model<select value={model} onChange={(event) => setModel(event.target.value)}>{models.map(([key, name]) => <option key={key} value={key}>{name}{providers.models[key]?.ready ? "" : " (offline)"}</option>)}</select></label>
      </div>
      <button type="button" className="button secondary full" disabled={pairs.length >= 4 || pairs.some((pair) => pair.store === store && pair.model === model)} onClick={() => onChange(addPair(pairs, { store, model }))}>Add comparison pair</button>
      <p className="hint">Select 2–4 pairs. Each pair gets its own index or collection.</p>
    </div>
  );
}

function ResultColumn({ result }) {
  return (
    <section className="result-column">
      <div className="result-head">
        <div><h3>{label(stores, result.pair.store)}</h3><p>{label(models, result.pair.model)}</p></div>
        <div className="timings"><span>Embed {result.embed_ms} ms</span><span>Search {result.search_ms} ms</span></div>
      </div>
      {result.error ? <p className="error">{result.error}</p> : null}
      {result.hits?.length ? result.hits.map((hit, index) => (
        <article className="hit" key={`${hit.document_id}-${index}`}>
          <div className="hit-top"><span className="rank">{String(index + 1).padStart(2, "0")}</span><strong>{hit.title}</strong><span className="score">{hit.score.toFixed(3)}</span></div>
          <p>{hit.text}</p>
          <small>Document: {hit.document_id}</small>
        </article>
      )) : !result.error ? <p className="empty">No matches returned.</p> : null}
    </section>
  );
}

function Metrics({ rows }) {
  if (!rows?.length) return null;
  return (
    <section className="metrics-panel" id="metrics">
      <div className="section-title"><div><h2>Evaluation</h2><p>10 judged queries from the fictional hiking corpus</p></div></div>
      <div className="table-wrap"><table><thead><tr><th>Pair</th><th>Recall@5</th><th>MRR@5</th><th>Index</th><th>Embed median</th><th>Search median</th></tr></thead><tbody>
        {rows.map((row) => <tr key={`${row.pair.store}-${row.pair.model}`}><td><strong>{label(stores, row.pair.store)}</strong><small>{label(models, row.pair.model)}</small></td><td>{(row.recall_at_5 * 100).toFixed(0)}%</td><td>{row.mrr_at_5.toFixed(3)}</td><td>{row.index_ms} ms</td><td>{row.median_embed_ms} ms</td><td>{row.median_search_ms} ms</td></tr>)}
      </tbody></table></div>
      <p className="hint">Scores and timings describe this small local test, not a production benchmark.</p>
    </section>
  );
}

export default function Home({ loaderData }) {
  const { corpora, runs, providers, selectedRun, selectedCorpusID, apiError } = loaderData;
  const selectedCorpus = corpora.find((corpus) => corpus.id === selectedCorpusID) || corpora.find((corpus) => corpus.id === selectedRun?.corpus_id) || corpora[0];
  const [pairs, setPairs] = useState([{ store: "postgres", model: "minilm" }, { store: "mongo", model: "minilm" }]);
  const actionData = useActionData();
  const searcher = useFetcher();
  const evaluator = useFetcher();
  const revalidator = useRevalidator();
  const pending = selectedRun?.pairs?.some((pair) => pair.status === "queued" || pair.status === "indexing");
  useEffect(() => { if (!pending) return; const timer = setInterval(() => revalidator.revalidate(), 2500); return () => clearInterval(timer); }, [pending, revalidator]);
  const readyCount = selectedRun?.pairs?.filter((pair) => pair.status === "ready").length || 0;
  const results = searcher.data?.result || selectedRun?.latest;
  return (
    <div className="app-shell">
      <header className="topbar"><div className="brand"><span className="brand-mark"><span /><span /><span /><span /></span><span>Vector Field Notes</span></div><div className="topbar-right"><span className="small-tag">Local comparison lab</span><span className="live-line" /></div></header>
      <div className="page-heading"><div><p className="heading-kicker">Semantic search, under the lens</p><h1>Find the trail.<br />Compare the search.</h1><p>Run the same documents and questions through different vector stores and embedding models. See what each combination returns.</p></div><div className="heading-art" aria-hidden="true"><div className="contour contour-one" /><div className="contour contour-two" /><div className="contour contour-three" /><span className="map-pin" /></div></div>
      {apiError && <div className="notice error"><strong>API unavailable.</strong> Start the Go server at <code>127.0.0.1:8080</code>. {apiError}</div>}
      <div className="workspace">
        <aside className="sidebar">
          <section className="setup-section"><div className="section-title"><span className="step">1</span><div><h2>Choose documents</h2><p>Use the sample or upload your own text.</p></div></div>
            {corpora.length ? <div className="corpus-list">{corpora.map((corpus) => <Link className={`corpus-option ${selectedCorpus?.id === corpus.id ? "selected" : ""}`} to={`/?corpus=${corpus.id}`} key={corpus.id}><span><strong>{corpus.name}</strong><small>{corpus.documents.length} documents {corpus.sample ? "· judged queries" : ""}</small></span><span className="radio" /></Link>)}</div> : <p className="empty">No corpora yet. Load the sample to begin.</p>}
            <Form method="post"><input type="hidden" name="intent" value="sample" /><button className="button primary full" type="submit">Load sample hiking corpus</button></Form>
            <Form method="post" encType="multipart/form-data" className="upload-form"><input type="hidden" name="intent" value="upload" /><label>Corpus name<input name="name" placeholder="My field notes" maxLength={100} /></label><label>Text or Markdown files<input name="files" type="file" accept=".txt,.md,text/plain,text/markdown" multiple required /></label><button className="button secondary full" type="submit">Upload documents</button></Form>
          </section>
          <section className="setup-section"><div className="section-title"><span className="step">2</span><div><h2>Build a comparison</h2><p>Choose a storage and model pair.</p></div></div><PairPicker pairs={pairs} onChange={setPairs} providers={providers} /><Form method="post"><input type="hidden" name="intent" value="run" /><input type="hidden" name="corpus_id" value={selectedCorpus?.id || ""} /><input type="hidden" name="pairs" value={JSON.stringify(pairs)} /><button className="button primary full" type="submit" disabled={!selectedCorpus || pairs.length < 2}>Index selected pairs</button></Form></section>
          <section className="setup-section status-section"><div className="section-title"><div><h2>Storage status</h2><p>Connections are checked when this page loads.</p></div></div><ProviderStatus providers={providers} /></section>
        </aside>
        <main className="main-panel">
          {actionData?.error && <div className="notice error">{actionData.error}</div>}
          <section className="runs-panel"><div className="section-title"><div><h2>Runs</h2><p>Saved comparisons remain available after restart.</p></div><span className="count">{runs.length}</span></div>
            {runs.length ? <div className="run-list">{runs.map((run) => <Link key={run.id} to={`/?run=${run.id}`} className={`run-item ${selectedRun?.id === run.id ? "active" : ""}`}><span><strong>{corpora.find((corpus) => corpus.id === run.corpus_id)?.name || "Corpus"}</strong><small>{run.pairs.length} pairs · {new Date(run.created_at).toLocaleString()}</small></span><span className="run-state">{run.pairs.every((pair) => pair.status === "ready") ? "Ready" : run.pairs.some((pair) => pair.status === "indexing" || pair.status === "queued") ? "Indexing" : "Needs attention"}</span></Link>)}</div> : <p className="empty">No runs yet. Select a corpus and index two pairs.</p>}
          </section>
          {selectedRun ? <>
            <section className="run-panel"><div className="section-title"><div><h2>Current run</h2><p>{selectedRun.id}</p></div><span className="count">{readyCount}/{selectedRun.pairs.length} ready</span></div><div className="run-pairs">{selectedRun.pairs.map((pair) => <div className="run-pair" key={`${pair.pair.store}-${pair.pair.model}`}><span className={`dot ${pair.status === "ready" ? "ready" : pair.status === "failed" ? "failed" : "pending"}`} /><span><strong>{label(stores, pair.pair.store)}</strong><small>{label(models, pair.pair.model)} · {pair.status}</small>{pair.error && <em>{pair.error}</em>}</span>{pair.index_ms ? <small>{pair.index_ms} ms</small> : null}</div>)}</div></section>
            <section className="search-panel"><div className="section-title"><span className="step">3</span><div><h2>Ask one question</h2><p>The same query runs across every ready pair.</p></div></div><searcher.Form method="post" className="search-form"><input type="hidden" name="intent" value="search" /><input type="hidden" name="run_id" value={selectedRun.id} /><label className="sr-only" htmlFor="query">Search query</label><input id="query" name="query" placeholder="Find a shady trail for a first-time hiker with a dog" required maxLength={1000} /><button className="button primary" disabled={!readyCount || searcher.state !== "idle"}>{searcher.state !== "idle" ? "Searching…" : "Search pairs"}</button></searcher.Form>{searcher.data?.error && <p className="error">{searcher.data.error}</p>}
              {results ? <div className="results-area"><div className="results-title"><h2>Results</h2><p>“{results.query}”</p></div><div className="results-grid">{results.results.map((result) => <ResultColumn key={`${result.pair.store}-${result.pair.model}`} result={result} />)}</div><p className="hint">Similarity scores come from different models and are not directly comparable.</p></div> : <div className="search-empty"><span className="search-glyph">⌕</span><strong>Results appear here</strong><p>Index a run, then ask a question to compare ranked passages.</p></div>}
            </section>
            {selectedCorpus?.sample && <section className="evaluate-panel"><div><h2>Evaluate the sample</h2><p>Run all 10 judged hiking questions through the ready pairs.</p></div><evaluator.Form method="post"><input type="hidden" name="intent" value="evaluate" /><input type="hidden" name="run_id" value={selectedRun.id} /><button className="button secondary" disabled={!readyCount || evaluator.state !== "idle"}>{evaluator.state !== "idle" ? "Evaluating…" : "Run evaluation"}</button></evaluator.Form></section>}{evaluator.data?.error && <p className="error">{evaluator.data.error}</p>}<Metrics rows={evaluator.data?.evaluation || selectedRun.latest_evaluation} />
          </> : <div className="welcome-panel"><div className="welcome-line" /><p>Start with a corpus, then choose two or more pairs. The first run will appear here with indexing progress and results.</p></div>}
        </main>
      </div>
      <footer>Vector Field Notes <span>·</span> Fictional sample trails <span>·</span> Local developer tool</footer>
    </div>
  );
}
