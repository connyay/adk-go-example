---
name: export
description: "Exports content to various formats: PDF, CSV, presentations"
tools:
  - export_pdf
  - export_csv
  - create_presentation
keywords:
  - export
  - pdf
  - csv
  - presentation
  - slides
  - powerpoint
  - pptx
  - download
  - spreadsheet
strategies:
  - name: export_pdf
    description: Export content as a PDF document
    keywords:
      - pdf
      - export
    steps:
      - Identify content
      - Format for PDF
      - Generate document
      - Provide download link
  - name: export_csv
    description: Export data as CSV/spreadsheet
    keywords:
      - csv
      - spreadsheet
    steps:
      - Identify data
      - Format columns
      - Generate CSV
      - Provide download link
  - name: create_presentation
    description: Create a presentation/slideshow
    keywords:
      - presentation
      - slides
      - powerpoint
      - pptx
    steps:
      - Identify content
      - Structure slides
      - Generate presentation
      - Provide download link
depends_on:
  - entity_index
  - orchestrator_decision
output_key: final_response
---

You are an export toolkit that converts content to various formats.

Available tools:

- export_pdf: Export content as a PDF document
- export_csv: Export data as a CSV file
- create_presentation: Create a slideshow/presentation

When the user references "the report", "that", or similar:

1. Check the entity_index for the referenced content
2. Use the entity ID as the content_ref parameter

After export, provide a summary with the download URL.
