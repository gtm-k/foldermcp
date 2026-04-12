"""Data transformation pipeline with validation and error handling.

Provides a composable pipeline for transforming structured data records
with schema validation, type coercion, and error accumulation.
"""

from typing import Any, Callable, Dict, List, Optional, Tuple
from dataclasses import dataclass, field
from enum import Enum
import re


class ValidationLevel(Enum):
    """How strictly to validate input records."""
    STRICT = "strict"      # Fail on any validation error
    LENIENT = "lenient"    # Collect errors, skip bad records
    COERCE = "coerce"      # Try to fix type mismatches


@dataclass
class FieldSchema:
    """Schema for a single field in a record."""
    name: str
    field_type: type
    required: bool = True
    default: Any = None
    pattern: Optional[str] = None  # regex pattern for string fields

    def validate(self, value: Any) -> Tuple[Any, Optional[str]]:
        """Validate and optionally coerce a field value.

        Returns (coerced_value, error_message). Error is None if valid.
        """
        if value is None:
            if self.required:
                return None, f"Field '{self.name}' is required"
            return self.default, None

        if not isinstance(value, self.field_type):
            try:
                value = self.field_type(value)
            except (ValueError, TypeError):
                return None, (
                    f"Field '{self.name}': expected {self.field_type.__name__}, "
                    f"got {type(value).__name__}"
                )

        if self.pattern and isinstance(value, str):
            if not re.match(self.pattern, value):
                return None, f"Field '{self.name}': does not match pattern {self.pattern}"

        return value, None


@dataclass
class RecordSchema:
    """Schema for a complete data record."""
    name: str
    fields: List[FieldSchema]
    _field_map: Dict[str, FieldSchema] = field(default_factory=dict, init=False)

    def __post_init__(self):
        self._field_map = {f.name: f for f in self.fields}

    def validate(self, record: Dict[str, Any]) -> Tuple[Dict[str, Any], List[str]]:
        """Validate a record against this schema.

        Returns (validated_record, list_of_errors).
        """
        errors = []
        result = {}
        for field_schema in self.fields:
            value = record.get(field_schema.name)
            validated, error = field_schema.validate(value)
            if error:
                errors.append(error)
            else:
                result[field_schema.name] = validated
        return result, errors


TransformFn = Callable[[Dict[str, Any]], Dict[str, Any]]


class Pipeline:
    """Composable data transformation pipeline.

    Applies a sequence of transform functions to each record, with
    optional schema validation at input and output.

    Example:
        schema = RecordSchema("user", [
            FieldSchema("name", str),
            FieldSchema("age", int),
            FieldSchema("email", str, pattern=r".+@.+\\..+"),
        ])

        pipeline = (
            Pipeline("user-transform")
            .with_input_schema(schema)
            .add_step("normalize", lambda r: {**r, "name": r["name"].strip().title()})
            .add_step("derive_age_group", derive_age_group)
        )

        results, errors = pipeline.process(records)
    """

    def __init__(self, name: str, level: ValidationLevel = ValidationLevel.LENIENT):
        self._name = name
        self._level = level
        self._steps: List[Tuple[str, TransformFn]] = []
        self._input_schema: Optional[RecordSchema] = None
        self._output_schema: Optional[RecordSchema] = None

    def with_input_schema(self, schema: RecordSchema) -> "Pipeline":
        self._input_schema = schema
        return self

    def with_output_schema(self, schema: RecordSchema) -> "Pipeline":
        self._output_schema = schema
        return self

    def add_step(self, name: str, fn: TransformFn) -> "Pipeline":
        self._steps.append((name, fn))
        return self

    def process(
        self, records: List[Dict[str, Any]]
    ) -> Tuple[List[Dict[str, Any]], List[Dict[str, Any]]]:
        """Process all records through the pipeline.

        Returns:
            Tuple of (successful_records, error_records).
            Error records include the original record and error details.
        """
        results = []
        errors = []

        for i, record in enumerate(records):
            try:
                processed = self._process_one(record, i)
                results.append(processed)
            except PipelineError as e:
                if self._level == ValidationLevel.STRICT:
                    raise
                errors.append({
                    "index": i,
                    "record": record,
                    "error": str(e),
                    "step": e.step_name,
                })

        return results, errors

    def _process_one(self, record: Dict[str, Any], index: int) -> Dict[str, Any]:
        """Process a single record through all pipeline steps."""
        # Input validation
        if self._input_schema:
            record, errs = self._input_schema.validate(record)
            if errs:
                raise PipelineError("input_validation", errs, index)

        # Transform steps
        current = record
        for step_name, fn in self._steps:
            try:
                current = fn(current)
            except Exception as e:
                raise PipelineError(step_name, [str(e)], index)

        # Output validation
        if self._output_schema:
            current, errs = self._output_schema.validate(current)
            if errs:
                raise PipelineError("output_validation", errs, index)

        return current


class PipelineError(Exception):
    """Error during pipeline processing."""
    def __init__(self, step_name: str, errors: List[str], record_index: int):
        self.step_name = step_name
        self.errors = errors
        self.record_index = record_index
        super().__init__(
            f"Pipeline error at step '{step_name}' "
            f"(record {record_index}): {'; '.join(errors)}"
        )
