# Equipment Log

A lightweight web-based equipment inventory log built with a simple **HTML/CSS/JavaScript frontend** and a **Python Flask backend**.

The application provides a clean interface for tracking equipment items, updating their status, adding notes, and managing the inventory through an admin edit mode.

This project is intended for small internal deployments or as a starting point for a more advanced inventory management tool.

---

# Features

- Clean single-page interface
- Fast search across multiple fields
- Inline status updates
- Inline note editing
- Admin edit mode for full inventory management
- Add and delete inventory entries
- JSON file-based storage (no database required)
- Automatic save backups
- Automatic hourly backups
- Edit locking to prevent conflicting changes

---

# Technology Stack

Frontend:
- HTML
- CSS
- JavaScript

Backend:
- Python
- Flask

Other libraries:
- filelock (for safe file access)

---

# Project structure

```text
.
├── server.py
├── requirements.txt
├── .env.example
├── .gitignore
├── README.md
└── app/
    ├── index.html
```


Runtime directories (created automatically when the server runs):

```text
app/
└── data/
├── inventory.json
├── backups/
└── hourly_backups/

```
---

# Requirements

- Python **3.10+**
- pip

---

# Installation

Clone the repository:


git clone https://github.com/RNYR/LogApp.git


Enter the project directory:


cd equipment-log


Install dependencies:


pip install -r requirements.txt


---

# Configuration

Before running the application, set the required environment variables.

These define the administrator login credentials and server port.

### Linux / macOS


export LOGAPP_ADMIN_USERNAME="admin"
export LOGAPP_ADMIN_PASSWORD="change-this-password"
export PORT="8787"


### Windows PowerShell


$env:LOGAPP_ADMIN_USERNAME="admin"
$env:LOGAPP_ADMIN_PASSWORD="change-this-password"
$env:PORT="8787"


---

# Running the Application

Start the Flask server:


python server.py


Open the application in your browser:


http://localhost:8787


---

# How the Application Works

The Flask backend serves a single-page frontend and provides API endpoints for managing inventory data.

Inventory information is stored in a JSON file and automatically backed up whenever changes are saved.

Two usage modes exist:

### Normal Mode

Users can:
- change equipment status
- update item notes

### Admin Mode

Admin users can:
- add new entries
- edit item name
- edit serial/part number
- edit equipment type
- edit status
- edit notes
- delete entries

Admin access requires the credentials defined in environment variables.

---

# Data Storage

Inventory data is stored in:


app/data/inventory.json


Backup files are automatically generated in:


app/data/backups/
app/data/hourly_backups/


Backups help recover previous inventory states if data becomes corrupted or accidentally modified.

---

# Locking System

The application includes a simple in-memory editing lock to prevent simultaneous conflicting writes.

Key behaviors:

- Only one editing session can hold the lock at a time
- Lock modes include `admin` and `normal`
- Locks expire automatically after a timeout
- Saving changes refreshes the lock expiration

Restarting the server clears any active lock.

---

# Deployment Notes

This project intentionally avoids using a database to keep setup simple.

For larger deployments you may want to extend it with:

- database storage (PostgreSQL, SQLite, etc.)
- user accounts and role management
- HTTPS support
- proper authentication pages
- audit logging
- API authentication tokens

---

# Running as a Windows Service

The application can be run as a background service using tools such as **NSSM**.

Typical setup:

1. Install Python and project dependencies
2. Configure environment variables for admin credentials
3. Create an NSSM service pointing to Python
4. Use `server.py` as the startup script
5. Set the working directory to the project folder

---

# Files to Exclude from Version Control

Do **not** commit the following files to GitHub:

- `.env`
- `app/data/inventory.json`
- `app/data/backups/`
- `app/data/hourly_backups/`
- temporary lock files
- any private inventory records

Use the provided `.gitignore` file to prevent accidental uploads.

---

# Security Notes

Before publishing this project or deploying it publicly:

- never commit real credentials
- remove any real inventory data
- avoid including serial numbers, customer names, or internal references
- use strong admin passwords
- consider replacing browser prompts with a proper login form

---

# License

This project is licensed under the **MIT License**.

See the `LICENSE` file for details.
