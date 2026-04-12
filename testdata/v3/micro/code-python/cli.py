"""Command-line interface for the data processing tool.

Uses argparse for CLI argument parsing with subcommands for
different operations: index, search, serve, and status.
"""

import argparse
import sys
import json
from typing import List, Optional


def create_parser() -> argparse.ArgumentParser:
    """Create the CLI argument parser with subcommands."""
    parser = argparse.ArgumentParser(
        prog="datatool",
        description="Data processing and search tool",
    )
    parser.add_argument(
        "--config", "-c",
        default="config.yaml",
        help="Path to configuration file",
    )
    parser.add_argument(
        "--verbose", "-v",
        action="count",
        default=0,
        help="Increase verbosity (-v, -vv, -vvv)",
    )

    subparsers = parser.add_subparsers(dest="command", help="Available commands")

    # index subcommand
    index_parser = subparsers.add_parser("index", help="Index files in a directory")
    index_parser.add_argument("path", help="Directory to index")
    index_parser.add_argument(
        "--force", "-f",
        action="store_true",
        help="Force re-index of already indexed files",
    )
    index_parser.add_argument(
        "--workers", "-w",
        type=int,
        default=4,
        help="Number of parallel indexing workers",
    )

    # search subcommand
    search_parser = subparsers.add_parser("search", help="Search indexed content")
    search_parser.add_argument("query", nargs="+", help="Search query terms")
    search_parser.add_argument(
        "--limit", "-n",
        type=int,
        default=10,
        help="Maximum number of results",
    )
    search_parser.add_argument(
        "--format",
        choices=["text", "json", "table"],
        default="text",
        help="Output format",
    )

    # serve subcommand
    serve_parser = subparsers.add_parser("serve", help="Start the gRPC server")
    serve_parser.add_argument(
        "--port", "-p",
        type=int,
        default=50051,
        help="Port to listen on",
    )
    serve_parser.add_argument(
        "--host",
        default="localhost",
        help="Host to bind to",
    )

    # status subcommand
    subparsers.add_parser("status", help="Show index status and statistics")

    return parser


def format_results(results: List[dict], fmt: str) -> str:
    """Format search results for display."""
    if fmt == "json":
        return json.dumps(results, indent=2)

    lines = []
    for i, result in enumerate(results, 1):
        path = result.get("path", "unknown")
        score = result.get("score", 0.0)
        snippet = result.get("snippet", "")
        lines.append(f"{i}. [{score:.3f}] {path}")
        if snippet:
            lines.append(f"   {snippet[:120]}")
        lines.append("")
    return "\n".join(lines)


def main(argv: Optional[List[str]] = None) -> int:
    """Main entry point for the CLI."""
    parser = create_parser()
    args = parser.parse_args(argv)

    if args.command is None:
        parser.print_help()
        return 1

    if args.command == "index":
        print(f"Indexing {args.path} with {args.workers} workers...")
        return 0

    if args.command == "search":
        query = " ".join(args.query)
        print(f"Searching for: {query}")
        return 0

    if args.command == "serve":
        print(f"Starting server on {args.host}:{args.port}")
        return 0

    if args.command == "status":
        print("Index status: OK")
        return 0

    return 1


if __name__ == "__main__":
    sys.exit(main())
