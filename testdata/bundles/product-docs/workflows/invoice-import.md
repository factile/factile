---
type: Workflow
title: Invoice Import Workflow
description: Describes how supplier invoice imports are processed.
tags: [invoices, suppliers, ocr]
resource: factile:test/product-docs/workflows/invoice-import
---

# Invoice Import Workflow

Supplier invoices are received, normalized, extracted, validated, and posted.

## Current Flow

1. Receive invoice image.
2. Normalize image.
3. Extract header and line items.
4. Validate totals.
5. Post accepted invoice.

## Related

- [OCR Failure Runbook](../runbooks/ocr-failure.md)
