# Equipment Log v2.0

Web-based equipment inventory and lending tracker for PEKAT VISION.

## Deployment on NAS via Docker

### Prerequisites
- Docker and Docker Compose installed on the NAS
- Access to the NAS command line or file manager

### Steps

1. **Upload** this folder to your NAS (e.g. `/volume1/docker/equipment-log/`)

2. **Navigate to the folder**
   ```bash
   cd /path/to/equipment-log
   ```

3. **Build the Docker image**
   ```bash
   docker compose build
   ```

4. **Start the container**
   ```bash
   docker compose up -d
   ```

5. **Access the app**
   Open a browser and go to: `http://<nas-ip>:80`

### Default Credentials

| Username | Password   |
|----------|------------|
| pekat    | traktor    |

**Change these after first login** via the admin panel.

### Data Storage

All data (inventory, options, email config, backups) is stored in:
```
app/data/
├── inventory.json     # Equipment items
├── options.json       # Types and statuses
├── email_config.json  # SMTP settings
└── backups/          # Automatic backups
    hourly_backups/    # Hourly backups (48 file retention)
```

The `app/data/` directory is mounted as a Docker volume, so data persists across container restarts and updates.

### Updating

To update to a new version:
1. Stop the container: `docker compose down`
2. Replace the application files (keep `app/data/` intact)
3. Rebuild and start: `docker compose build && docker compose up -d`

### Ports

The app is exposed on port **80** (mapped to internal port 8787).

### Due-Date Email Alerts

The app automatically checks for overdue equipment daily at **11:00** (configurable via `CHECK_HOUR` environment variable in `docker-compose.yml`). Configure SMTP settings in the app's **Alert Settings** panel (admin only).

### Optional: mDNS Discovery

The folder includes `avahi-services/logapp.service` for automatic network discovery. On systems with Avahi installed, the app will appear as "sklad" on your network.
