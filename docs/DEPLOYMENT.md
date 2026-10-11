# LockForge – Deployment Guide (Online & GitHub)

This guide walks you through deploying **LockForge** to your GitHub account and publishing the live web service online so anyone can access your interactive distributed lock dashboard and gRPC endpoints.

---

## Part 1: Push Project to GitHub

### Option A: Using the GitHub CLI (`gh`) (Quickest)

If you have `gh` installed:
```bash
# 1. Login to your GitHub account
gh auth login

# 2. Create the repository and push automatically
cd /Users/siddhijadhav/.gemini/antigravity/scratch/lockforge
gh repo create lockforge --public --source=. --remote=origin --push
```

### Option B: Using Git & GitHub Web UI

1. Go to [github.com/new](https://github.com/new) and create a new repository named **`lockforge`** (do not initialize with README or .gitignore, as we already have them).
2. Run the following in your terminal:

```bash
cd /Users/siddhijadhav/.gemini/antigravity/scratch/lockforge

# Set your GitHub repository URL (replace <your-username> with your GitHub username)
git remote add origin https://github.com/<your-username>/lockforge.git

# Push to the main branch
git push -u origin main
```

Once pushed, GitHub will automatically trigger the **GitHub Actions CI Pipeline** (`.github/workflows/ci.yml`) to run tests and verify the build!

---

## Part 2: Deploying Online (Free Cloud Hosting)

LockForge packages both the Go backend and the compiled React 19 dashboard into a single lightweight Docker container that serves HTTP port `8080`.

### Option 1: Render (Recommended – 1-Click Free Hosting)

1. Go to [dashboard.render.com](https://dashboard.render.com) and sign in with your GitHub account.
2. Click **New +** -> **Web Service**.
3. Select your **`lockforge`** repository.
4. Render will automatically detect the `Dockerfile` (or `render.yaml`):
   - **Environment**: Docker
   - **Port**: `8080`
   - **Plan**: Free
5. Click **Create Web Service**.

Render will build and deploy the container, providing you with a live HTTPS URL (e.g., `https://lockforge.onrender.com`).

---

### Option 2: Railway

1. Go to [railway.app](https://railway.app/) and sign in with GitHub.
2. Click **New Project** -> **Deploy from GitHub repo**.
3. Select **`lockforge`**.
4. Railway will automatically detect the Dockerfile and `railway.json`.
5. Under Settings, click **Generate Domain** to get a public URL (e.g., `https://lockforge.up.railway.app`).

---

### Option 3: Fly.io

If you use the `flyctl` command line:
```bash
# Authenticate with Fly.io
fly auth login

# Launch app using included fly.toml
fly launch --now
```

---

### Option 4: Deploying on a Cloud VPS (AWS EC2, DigitalOcean, Hetzner, GCP)

You can run the full multi-node etcd cluster, Redis, backend, and frontend via Docker Compose:

```bash
# Clone the repository on your server
git clone https://github.com/<your-username>/lockforge.git
cd lockforge

# Start full multi-node cluster
docker compose up -d
```

- **React Dashboard**: `http://<your-server-ip>:3000`
- **Backend API & SSE**: `http://<your-server-ip>:8080`
- **gRPC Service**: `http://<your-server-ip>:50051`
