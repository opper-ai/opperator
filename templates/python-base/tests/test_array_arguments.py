#!/usr/bin/env python3
"""
Test script for array argument validation feature.

This script tests the new array argument support including:
- Array of strings
- Array of integers
- Array of objects with nested validation
- Nested arrays
"""

import sys
import os

# Add the parent directory (python-base) to path
sys.path.insert(0, os.path.dirname(os.path.dirname(__file__)))

from opperator.agent import OpperatorAgent


class TestAgent(OpperatorAgent):
    def initialize(self):
        """Required initialize method"""
        pass

    def start(self):
        """Required start method"""
        pass

    def _coerce_argument_value(self, arg_type, value, items=None, properties=None):
        # Use the parent implementation
        return super()._coerce_argument_value(arg_type, value, items, properties)


def test_array_of_strings():
    """Test array of strings validation"""
    print("Testing array of strings...")
    agent = TestAgent()

    # Test valid array
    result = agent._coerce_argument_value(
        "array",
        ["tag1", "tag2", "tag3"],
        items={"type": "string"}
    )
    assert result == ["tag1", "tag2", "tag3"], f"Expected ['tag1', 'tag2', 'tag3'], got {result}"

    # Test coercion from numbers to strings
    result = agent._coerce_argument_value(
        "array",
        [1, 2, 3],
        items={"type": "string"}
    )
    assert result == ["1", "2", "3"], f"Expected ['1', '2', '3'], got {result}"

    print("✓ Array of strings validation passed")


def test_array_of_integers():
    """Test array of integers validation"""
    print("Testing array of integers...")
    agent = TestAgent()

    # Test valid array
    result = agent._coerce_argument_value(
        "array",
        [1, 2, 3],
        items={"type": "integer"}
    )
    assert result == [1, 2, 3], f"Expected [1, 2, 3], got {result}"

    # Test coercion from strings to integers
    result = agent._coerce_argument_value(
        "array",
        ["1", "2", "3"],
        items={"type": "integer"}
    )
    assert result == [1, 2, 3], f"Expected [1, 2, 3], got {result}"

    # Test coercion from floats to integers
    result = agent._coerce_argument_value(
        "array",
        [1.0, 2.0, 3.0],
        items={"type": "integer"}
    )
    assert result == [1, 2, 3], f"Expected [1, 2, 3], got {result}"

    print("✓ Array of integers validation passed")


def test_array_of_objects():
    """Test array of objects validation"""
    print("Testing array of objects...")
    agent = TestAgent()

    # Test valid array of objects
    result = agent._coerce_argument_value(
        "array",
        [
            {"name": "Alice", "age": 30},
            {"name": "Bob", "age": 25},
        ],
        items={
            "type": "object",
            "properties": {
                "name": {"type": "string"},
                "age": {"type": "integer"},
            }
        }
    )
    assert len(result) == 2, f"Expected 2 items, got {len(result)}"
    assert result[0]["name"] == "Alice", f"Expected 'Alice', got {result[0]['name']}"
    assert result[0]["age"] == 30, f"Expected 30, got {result[0]['age']}"

    # Test coercion within objects
    result = agent._coerce_argument_value(
        "array",
        [
            {"name": "Charlie", "age": "35"},  # age as string
        ],
        items={
            "type": "object",
            "properties": {
                "name": {"type": "string"},
                "age": {"type": "integer"},
            }
        }
    )
    assert result[0]["age"] == 35, f"Expected 35 (int), got {result[0]['age']}"
    assert isinstance(result[0]["age"], int), f"Expected int type, got {type(result[0]['age'])}"

    print("✓ Array of objects validation passed")


def test_nested_arrays():
    """Test nested array structures"""
    print("Testing nested arrays...")
    agent = TestAgent()

    # Test 2D array (array of arrays)
    result = agent._coerce_argument_value(
        "array",
        [[1, 2, 3], [4, 5, 6]],
        items={
            "type": "array",
            "items": {"type": "number"}
        }
    )
    assert len(result) == 2, f"Expected 2 rows, got {len(result)}"
    assert result[0] == [1, 2, 3], f"Expected [1, 2, 3], got {result[0]}"
    assert result[1] == [4, 5, 6], f"Expected [4, 5, 6], got {result[1]}"

    # Test coercion in nested arrays
    result = agent._coerce_argument_value(
        "array",
        [["1", "2"], ["3", "4"]],
        items={
            "type": "array",
            "items": {"type": "integer"}
        }
    )
    assert result[0] == [1, 2], f"Expected [1, 2], got {result[0]}"
    assert result[1] == [3, 4], f"Expected [3, 4], got {result[1]}"

    print("✓ Nested arrays validation passed")


def test_array_from_json_string():
    """Test parsing arrays from JSON strings"""
    print("Testing array parsing from JSON strings...")
    agent = TestAgent()

    # Test parsing array from JSON string
    result = agent._coerce_argument_value(
        "array",
        '["a", "b", "c"]',
        items={"type": "string"}
    )
    assert result == ["a", "b", "c"], f"Expected ['a', 'b', 'c'], got {result}"

    # Test parsing array of numbers
    result = agent._coerce_argument_value(
        "array",
        '[1, 2, 3]',
        items={"type": "integer"}
    )
    assert result == [1, 2, 3], f"Expected [1, 2, 3], got {result}"

    print("✓ Array parsing from JSON strings passed")


def test_error_handling():
    """Test error handling for invalid inputs"""
    print("Testing error handling...")
    agent = TestAgent()

    # Test invalid item type
    try:
        agent._coerce_argument_value(
            "array",
            ["a", "b", "c"],
            items={"type": "integer"}
        )
        assert False, "Should have raised ValueError for invalid integer"
    except ValueError as e:
        assert "invalid" in str(e).lower(), f"Expected error about invalid item, got: {e}"

    # Test missing required object property
    try:
        agent._coerce_argument_value(
            "array",
            [{"name": "Alice"}],  # missing required 'age'
            items={
                "type": "object",
                "properties": {
                    "name": {"type": "string"},
                    "age": {"type": "integer", "required": True},
                }
            }
        )
        assert False, "Should have raised ValueError for missing required property"
    except ValueError as e:
        assert "required" in str(e).lower() or "missing" in str(e).lower(), \
            f"Expected error about required property, got: {e}"

    print("✓ Error handling tests passed")


if __name__ == "__main__":
    print("=" * 60)
    print("Array Argument Validation Tests")
    print("=" * 60)
    print()

    try:
        test_array_of_strings()
        test_array_of_integers()
        test_array_of_objects()
        test_nested_arrays()
        test_array_from_json_string()
        test_error_handling()

        print()
        print("=" * 60)
        print("✓ All tests passed!")
        print("=" * 60)
        sys.exit(0)
    except Exception as e:
        print()
        print("=" * 60)
        print(f"✗ Test failed: {e}")
        print("=" * 60)
        import traceback
        traceback.print_exc()
        sys.exit(1)
