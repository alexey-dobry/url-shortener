
import http from "k6/http";
import { check } from "k6";

const BASE = __ENV.BASE || "http://localhost:8080";

export const options = {
  scenarios: {
    sustained_load: {
      executor: "constant-arrival-rate",
      rate: 50,            // 50 запросов в секунду
      timeUnit: "1s",
      duration: "60s",
      preAllocatedVUs: 50,
      maxVUs: 200,
    },
  },
  thresholds: {
    http_req_failed: ["rate<0.01"],
    http_req_duration: ["p(95)<500"],
  },
};

export default function () {
  const r = http.get(`${BASE}/healthz`, { timeout: "5s" });
  check(r, {
    "status 200": (res) => res.status === 200,
    "got served_by": (res) => {
      try { return !!res.json("instance"); } catch (_) { return false; }
    },
  });
}

