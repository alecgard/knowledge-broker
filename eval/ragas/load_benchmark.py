#!/usr/bin/env python3
"""Load standard benchmark datasets and convert them to KB's testset format.

Supports BEIR datasets (nfcorpus, scifact, fiqa, arguana) and HotpotQA.
Outputs a testset.json and individual corpus text files compatible with
the Knowledge Broker eval pipeline.
"""

import argparse
import json
import sys
from pathlib import Path


def load_beir(dataset_name: str, max_queries: int, max_corpus: int) -> tuple:
    """Load a BEIR dataset and return (queries, corpus) pairs.

    Returns:
        queries: list of dicts with id, query, expected_sources, reference_answer
        corpus_docs: list of dicts with doc_id, title, text
    """
    from datasets import load_dataset

    # BEIR datasets are hosted on HuggingFace as BeIR/<name>
    hf_name = f"BeIR/{dataset_name}"

    print(f"Loading BEIR corpus from {hf_name}...")
    corpus_ds = load_dataset(hf_name, "corpus", split="corpus")

    print(f"Loading BEIR queries from {hf_name}...")
    queries_ds = load_dataset(hf_name, "queries", split="queries")

    # Load qrels to map queries to relevant documents
    print(f"Loading BEIR qrels from {hf_name}...")
    try:
        qrels_ds = load_dataset(hf_name, "default", split="test")
    except Exception:
        # Some BEIR datasets use different split names
        qrels_ds = load_dataset(hf_name, "default", split="validation")

    # Build corpus lookup (limited)
    corpus_lookup = {}
    for i, doc in enumerate(corpus_ds):
        if i >= max_corpus:
            break
        doc_id = doc["_id"]
        corpus_lookup[doc_id] = {
            "doc_id": doc_id,
            "title": doc.get("title", ""),
            "text": doc.get("text", ""),
        }

    # Build qrels mapping: query_id -> list of relevant corpus_ids
    qrels = {}
    for row in qrels_ds:
        qid = row["query-id"]
        cid = row["corpus-id"]
        score = row.get("score", 1)
        if score > 0:
            qrels.setdefault(qid, []).append(cid)

    # Build queries lookup
    queries_lookup = {}
    for q in queries_ds:
        queries_lookup[q["_id"]] = q["text"]

    # Build testset entries
    queries = []
    count = 0
    for qid, relevant_cids in qrels.items():
        if count >= max_queries:
            break
        if qid not in queries_lookup:
            continue

        # Only include queries whose relevant docs are in our corpus subset
        available_cids = [cid for cid in relevant_cids if cid in corpus_lookup]
        if not available_cids:
            continue

        # Use the first relevant doc's text as a reference answer stand-in
        ref_doc = corpus_lookup[available_cids[0]]
        ref_text = ref_doc["text"]
        if ref_doc["title"]:
            ref_text = f"{ref_doc['title']}. {ref_text}"

        queries.append(
            {
                "id": f"bench_{count + 1:03d}",
                "query": queries_lookup[qid],
                "expected_sources": [f"{cid}.txt" for cid in available_cids],
                "reference_answer": ref_text,
                "category": "benchmark",
            }
        )
        count += 1

    corpus_docs = list(corpus_lookup.values())
    return queries, corpus_docs


def load_hotpotqa(max_queries: int, max_corpus: int) -> tuple:
    """Load HotpotQA dataset and return (queries, corpus) pairs.

    Returns:
        queries: list of dicts with id, query, expected_sources, reference_answer
        corpus_docs: list of dicts with doc_id, title, text
    """
    from datasets import load_dataset

    print("Loading HotpotQA...")
    ds = load_dataset("hotpot_qa", "distractor", split="validation")

    corpus_docs_map = {}
    queries = []
    count = 0

    for item in ds:
        if count >= max_queries:
            break

        question = item["question"]
        answer = item["answer"]

        # Each HotpotQA item has supporting context paragraphs
        context_titles = item["context"]["title"]
        context_sentences = item["context"]["sentences"]

        source_files = []
        for title, sentences in zip(context_titles, context_sentences):
            doc_id = title.replace(" ", "_").replace("/", "_")
            text = " ".join(sentences)

            if doc_id not in corpus_docs_map and len(corpus_docs_map) < max_corpus:
                corpus_docs_map[doc_id] = {
                    "doc_id": doc_id,
                    "title": title,
                    "text": text,
                }
            if doc_id in corpus_docs_map:
                source_files.append(f"{doc_id}.txt")

        if not source_files:
            continue

        queries.append(
            {
                "id": f"bench_{count + 1:03d}",
                "query": question,
                "expected_sources": source_files,
                "reference_answer": answer,
                "category": "benchmark",
            }
        )
        count += 1

    corpus_docs = list(corpus_docs_map.values())
    return queries, corpus_docs


def write_output(
    queries: list, corpus_docs: list, output_dir: Path
) -> None:
    """Write testset.json and corpus text files to the output directory."""
    output_dir.mkdir(parents=True, exist_ok=True)
    corpus_dir = output_dir / "corpus"
    corpus_dir.mkdir(exist_ok=True)

    # Write testset.json
    testset_path = output_dir / "testset.json"
    with open(testset_path, "w") as f:
        json.dump(queries, f, indent=2)
    print(f"Wrote {len(queries)} test cases to {testset_path}")

    # Write individual corpus files
    for doc in corpus_docs:
        doc_path = corpus_dir / f"{doc['doc_id']}.txt"
        content = ""
        if doc.get("title"):
            content = f"{doc['title']}\n\n"
        content += doc["text"]
        with open(doc_path, "w") as f:
            f.write(content)
    print(f"Wrote {len(corpus_docs)} corpus documents to {corpus_dir}")


def run(args: argparse.Namespace) -> None:
    """Main benchmark loading pipeline."""
    benchmark = args.benchmark.lower()
    output_dir = Path(args.output_dir)

    if benchmark.startswith("beir/"):
        dataset_name = benchmark.split("/", 1)[1]
        supported = ("nfcorpus", "scifact", "fiqa", "arguana")
        if dataset_name not in supported:
            print(
                f"Error: unsupported BEIR dataset '{dataset_name}'. "
                f"Supported: {', '.join(supported)}",
                file=sys.stderr,
            )
            sys.exit(1)
        queries, corpus_docs = load_beir(
            dataset_name, args.max_queries, args.max_corpus
        )

    elif benchmark == "hotpotqa":
        queries, corpus_docs = load_hotpotqa(args.max_queries, args.max_corpus)

    else:
        print(
            f"Error: unknown benchmark '{benchmark}'. "
            "Supported: beir/nfcorpus, beir/scifact, beir/fiqa, beir/arguana, hotpotqa",
            file=sys.stderr,
        )
        sys.exit(1)

    if not queries:
        print("Warning: no queries produced. Check dataset availability.", file=sys.stderr)
        sys.exit(1)

    write_output(queries, corpus_docs, output_dir)
    print(f"\nBenchmark '{benchmark}' loaded successfully.")
    print(f"  Queries:  {len(queries)}")
    print(f"  Corpus:   {len(corpus_docs)} documents")


def main():
    parser = argparse.ArgumentParser(
        description="Load standard benchmarks into KB's testset format"
    )
    parser.add_argument(
        "--benchmark",
        required=True,
        help="Benchmark name (beir/nfcorpus, beir/scifact, beir/fiqa, beir/arguana, hotpotqa)",
    )
    parser.add_argument(
        "--output-dir",
        required=True,
        help="Directory to write testset.json and corpus files",
    )
    parser.add_argument(
        "--max-queries",
        type=int,
        default=100,
        help="Maximum number of test queries to include (default: 100)",
    )
    parser.add_argument(
        "--max-corpus",
        type=int,
        default=500,
        help="Maximum number of corpus documents (default: 500)",
    )

    args = parser.parse_args()
    run(args)


if __name__ == "__main__":
    main()
