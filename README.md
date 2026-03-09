# LogApp

A lightweight web-based equipment inventory log with a simple HTML/CSS/JavaScript frontend and a Flask backend.

This project is designed for small internal setups or as a starting point for a more advanced inventory tool. It uses JSON file storage, supports inline updates, provides an admin edit mode, and creates automatic backups when data is saved.

## Features

- Clean single-page interface
- Search by:
  - item ID
  - item name
  - serial/part number
  - type
  - status
  - note
- Inline status updates
- Inline note updates
- Admin mode for full editing
- Add and delete entries
- JSON-based storage
- Automatic save backups
- Automatic hourly backups
- Simple edit locking to reduce conflicting writes

## Tech stack

- Frontend: HTML, CSS, JavaScript
- Backend: Python, Flask
- File locking: `filelock`

## Project structure

```text
.
├── server.py
├── requirements.txt
├── .env.example
└── app/
    ├── index.html
    └── data/
        ├── inventory.json
        ├── backups/
        └── hourly_backups/
