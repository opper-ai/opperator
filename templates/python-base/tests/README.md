# Opperator SDK Tests

This directory contains test suites for the Opperator Python SDK.

## Running Tests

### Run all tests
```bash
cd templates/python-base/tests
python3 test_array_arguments.py
```

### Run individual test file
```bash
python3 -m pytest test_array_arguments.py  # If pytest is installed
# OR
python3 test_array_arguments.py  # Direct execution
```

## Test Coverage

### test_array_arguments.py
Tests for typed array argument validation and coercion:
- Array of strings validation
- Array of integers validation and coercion
- Array of objects with nested property validation
- Nested array structures (2D arrays)
- JSON string parsing
- Error handling for invalid inputs

## Adding New Tests

When adding new tests:
1. Create a new test file following the pattern `test_<feature>.py`
2. Import required SDK components from `opperator`
3. Add docstrings explaining what's being tested
4. Update this README with the new test description
