# RAGAS Evaluation Harness

Runs [RAGAS](https://docs.ragas.io/) metrics on Knowledge Broker eval exports and loads standard IR benchmarks for comparison.

## Prerequisites

- Python 3.10+
- An `ANTHROPIC_API_KEY` environment variable (used for both answer generation and RAGAS LLM-based metrics)

## Setup

```bash
cd eval/ragas
python -m venv .venv
source .venv/bin/activate
pip install -r requirements.txt
```

## Running RAGAS Evaluation

First, export eval data from the Go eval system in RAGAS format:

```bash
make eval-ragas-export   # produces eval/ragas-export.json
```

Then run the RAGAS harness:

```bash
python run_ragas.py -i ../ragas-export.json -o results.json --verbose
```

Or use the Makefile target from the project root:

```bash
make eval-ragas
```

### CLI Options

| Flag | Description | Default |
|------|-------------|---------|
| `--input` / `-i` | Path to RAGAS export JSON (required) | - |
| `--output` / `-o` | Path to save combined results JSON | - |
| `--metrics` | Comma-separated metric names | all |
| `--llm-model` | Anthropic model for RAGAS evaluation | claude-sonnet-4-20250514 |
| `--verbose` | Print per-question score breakdown | off |

### Available Metrics

| Metric | What it measures |
|--------|-----------------|
| **faithfulness** | Whether the answer is grounded in the retrieved contexts (no hallucination) |
| **answer_relevancy** | Whether the answer addresses the question that was asked |
| **context_precision** | Whether the relevant context chunks are ranked higher than irrelevant ones |
| **context_recall** | Whether the retrieved contexts cover the information in the ground truth |
| **answer_correctness** | Factual overlap between the generated answer and the reference answer |

These are industry-standard RAG evaluation metrics. Typical production targets:
- faithfulness >= 0.85
- answer_relevancy >= 0.80
- context_precision >= 0.70
- context_recall >= 0.75
- answer_correctness >= 0.70

The harness also reports KB-specific signals (avg_freshness, avg_confidence) alongside RAGAS scores for a combined view.

## Loading Benchmarks

Load standard IR/QA benchmarks into KB's testset format for end-to-end evaluation:

```bash
python load_benchmark.py --benchmark beir/nfcorpus --output-dir ../benchmark-nfcorpus
python load_benchmark.py --benchmark beir/scifact --output-dir ../benchmark-scifact
python load_benchmark.py --benchmark hotpotqa --output-dir ../benchmark-hotpotqa --max-queries 50
```

### Supported Benchmarks

| Benchmark | Domain | Description |
|-----------|--------|-------------|
| `beir/nfcorpus` | Biomedical | Nutrition and fitness queries over PubMed abstracts |
| `beir/scifact` | Scientific | Fact verification against scientific abstracts |
| `beir/fiqa` | Finance | Financial opinion QA |
| `beir/arguana` | Arguments | Counterargument retrieval |
| `hotpotqa` | Wikipedia | Multi-hop reasoning questions |

Each benchmark produces a `testset.json` and a `corpus/` directory that can be ingested and evaluated through KB's standard pipeline.
