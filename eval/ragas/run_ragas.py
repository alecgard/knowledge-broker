#!/usr/bin/env python3
"""Run RAGAS evaluation on Knowledge Broker export data.

Loads a RAGAS-format JSON file exported by the Go eval system,
runs RAGAS metrics, merges KB-specific confidence signals, and
produces a combined report.
"""

import argparse
import json
import sys
from pathlib import Path

from ragas import EvaluationDataset, SingleTurnSample, evaluate
from ragas.metrics import (
    answer_correctness,
    answer_relevancy,
    context_precision,
    context_recall,
    faithfulness,
)

AVAILABLE_METRICS = {
    "faithfulness": faithfulness,
    "answer_relevancy": answer_relevancy,
    "context_precision": context_precision,
    "context_recall": context_recall,
    "answer_correctness": answer_correctness,
}


def load_export(path: Path) -> dict:
    """Load the RAGAS export JSON produced by the Go eval system."""
    with open(path) as f:
        data = json.load(f)

    if "dataset" not in data:
        print("Error: export JSON missing 'dataset' key", file=sys.stderr)
        sys.exit(1)

    return data


def build_dataset(items: list[dict]) -> EvaluationDataset:
    """Convert export items into a RAGAS EvaluationDataset."""
    samples = []
    for item in items:
        samples.append(
            SingleTurnSample(
                user_input=item["question"],
                response=item["answer"],
                retrieved_contexts=item["contexts"],
                reference=item["ground_truth"],
            )
        )
    return EvaluationDataset(samples=samples)


def extract_kb_scores(items: list[dict]) -> dict:
    """Aggregate KB-specific confidence signals across all questions."""
    freshness_vals = [
        item["kb_avg_freshness"] for item in items if "kb_avg_freshness" in item
    ]
    confidence_vals = [
        item["kb_avg_confidence"] for item in items if "kb_avg_confidence" in item
    ]

    scores = {}
    if freshness_vals:
        scores["avg_freshness"] = sum(freshness_vals) / len(freshness_vals)
    if confidence_vals:
        scores["avg_confidence"] = sum(confidence_vals) / len(confidence_vals)

    return scores


def build_per_question(items: list[dict], ragas_df) -> list[dict]:
    """Merge per-question RAGAS scores with KB metadata."""
    per_question = []
    for i, item in enumerate(items):
        entry = {
            "id": item.get("kb_id", f"q{i:02d}"),
            "category": item.get("kb_category", "unknown"),
            "question": item["question"],
        }

        # Add RAGAS per-row scores from the result dataframe
        if ragas_df is not None and i < len(ragas_df):
            row = ragas_df.iloc[i]
            for metric_name in AVAILABLE_METRICS:
                if metric_name in row:
                    entry[metric_name] = float(row[metric_name])

        # Add KB-specific signals
        if "kb_avg_freshness" in item:
            entry["kb_avg_freshness"] = item["kb_avg_freshness"]
        if "kb_avg_confidence" in item:
            entry["kb_avg_confidence"] = item["kb_avg_confidence"]

        per_question.append(entry)

    return per_question


def run(args: argparse.Namespace) -> None:
    """Main evaluation pipeline."""
    data = load_export(Path(args.input))
    items = data["dataset"]
    metadata = data.get("metadata", {})

    # Resolve which metrics to run
    if args.metrics:
        metric_names = [m.strip() for m in args.metrics.split(",")]
        metrics = []
        for name in metric_names:
            if name not in AVAILABLE_METRICS:
                print(
                    f"Error: unknown metric '{name}'. "
                    f"Available: {', '.join(AVAILABLE_METRICS)}",
                    file=sys.stderr,
                )
                sys.exit(1)
            metrics.append(AVAILABLE_METRICS[name])
    else:
        metric_names = list(AVAILABLE_METRICS.keys())
        metrics = list(AVAILABLE_METRICS.values())

    # Configure LLM for RAGAS evaluation (Claude by default)
    from langchain_anthropic import ChatAnthropic

    llm = ChatAnthropic(model=args.llm_model)
    llm_kwargs = {"llm": llm}

    # Build dataset and evaluate
    dataset = build_dataset(items)
    print(f"Running RAGAS evaluation on {len(items)} questions...")
    print(f"Metrics: {', '.join(metric_names)}")
    print()

    result = evaluate(dataset=dataset, metrics=metrics, **llm_kwargs)

    # Extract aggregate RAGAS scores
    ragas_scores = {}
    for name in metric_names:
        if name in result:
            ragas_scores[name] = round(float(result[name]), 4)

    # Extract KB scores
    kb_scores = extract_kb_scores(items)

    # Build per-question breakdown
    ragas_df = result.to_pandas() if hasattr(result, "to_pandas") else None
    per_question = build_per_question(items, ragas_df)

    # Assemble combined output
    combined = {
        "ragas_scores": ragas_scores,
        "kb_scores": kb_scores,
        "per_question": per_question,
        "metadata": metadata,
    }

    # Print report
    print("=" * 60)
    print("RAGAS Evaluation Results")
    print("=" * 60)
    print()
    print("RAGAS Scores:")
    for name, score in ragas_scores.items():
        print(f"  {name:25s} {score:.4f}")
    print()
    print("KB Scores:")
    for name, score in kb_scores.items():
        print(f"  {name:25s} {score:.4f}")
    print()

    if args.verbose:
        print("-" * 60)
        print("Per-Question Breakdown:")
        print("-" * 60)
        for entry in per_question:
            print(f"\n  [{entry['id']}] ({entry['category']}) {entry['question']}")
            for key, val in entry.items():
                if key in ("id", "category", "question"):
                    continue
                if isinstance(val, float):
                    print(f"    {key:25s} {val:.4f}")

    # Save output if requested
    if args.output:
        output_path = Path(args.output)
        with open(output_path, "w") as f:
            json.dump(combined, f, indent=2)
        print(f"\nResults saved to {output_path}")


def main():
    parser = argparse.ArgumentParser(
        description="Run RAGAS evaluation on Knowledge Broker export data"
    )
    parser.add_argument(
        "--input",
        "-i",
        required=True,
        help="Path to RAGAS export JSON from Go eval system",
    )
    parser.add_argument(
        "--output",
        "-o",
        help="Path to save combined results JSON",
    )
    parser.add_argument(
        "--metrics",
        help="Comma-separated list of RAGAS metrics to run (default: all)",
    )
    parser.add_argument(
        "--llm-model",
        default="claude-sonnet-4-20250514",
        help="Anthropic model for RAGAS evaluation (default: claude-sonnet-4-20250514)",
    )
    parser.add_argument(
        "--verbose",
        action="store_true",
        help="Print per-question scores",
    )

    args = parser.parse_args()
    run(args)


if __name__ == "__main__":
    main()
