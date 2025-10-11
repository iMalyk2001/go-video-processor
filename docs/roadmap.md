# 🚀 Project Roadmap

This document outlines the development roadmap for the Go-based Video Processing Web Application.

---

## 🎯 Phase 1: MVP (2–3 weeks)
**Goal:** Process one video end-to-end locally.

- [ ] Simple upload UI (React/Next.js)
- [ ] Go backend (Fiber/Gin) with `/upload` and `/status/:id`
- [ ] Redis queue for jobs
- [ ] Go worker using FFmpeg to transcode (e.g., 720p)
- [ ] Store results in MinIO
- [ ] Docker Compose setup

**Milestone:** `v0.1.0` – Upload → Process → Download ✅

---

## 🧱 Phase 2: Stability + CI/CD (2–3 weeks)
**Goal:** Reliable and deployable version.

- [ ] Add Postgres for job tracking
- [ ] CI/CD with GitHub Actions
- [ ] Logging + error tracking
- [ ] Deploy to Render / Railway / Fly.io
- [ ] Basic metrics

**Milestone:** `v0.2.0` – Deployed MVP ✅

---

## ⚙️ Phase 3: Scaling Foundations (4–6 weeks)
**Goal:** Handle 1k+ concurrent jobs.

- [ ] Move workers to Kubernetes / Cloud Run
- [ ] Autoscaling
- [ ] Job presets (720p, 1080p, thumbnails)
- [ ] Rate limiting per user
- [ ] Monitoring (Prometheus / Grafana)

**Milestone:** `v0.3.0` – Load-tested ✅

---

## 💼 Phase 4: Business Scale
**Goal:** Production-grade system.

- [ ] Auth (OAuth / JWT)
- [ ] Billing + quotas
- [ ] CDN (Cloudflare / CloudFront)
- [ ] Video moderation
- [ ] HLS packaging

**Milestone:** `v1.0.0` – Public release ✅

---

## 📁 Repo Overview

| Folder | Description |
|--------|--------------|
| `frontend/` | React/Next.js UI |
| `backend/` | Go API |
| `worker/` | FFmpeg worker |
| `infra/` | Docker + deployment files |
| `docs/` | Documentation |
| `.github/` | CI/CD workflows |

---

📌 Stay consistent: implement one checklist per week → commit → tag → test 🚦
