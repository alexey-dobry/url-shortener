
import http from "k6/http";
import { check, sleep } from "k6";
import { Counter } from "k6/metrics";

const BASE = __ENV.BASE || "http://localhost:8080";

const servedByInstance = new Counter("served_by_instance");

export const options = {
  scenarios: {
    health_spread: {
      executor: "constant-vus",
      vus: 10,
      duration: "20s",
      exec: "hammerHealth",
    },
    auth_journey: {
      executor: "per-vu-iterations",
      vus: 10,
      iterations: 5,
      maxDuration: "30s",
      exec: "userJourney",
    },
  },
  thresholds: {
    http_req_failed: ["rate<0.01"],
    http_req_duration: ["p(95)<500"],
  },
};

export function hammerHealth() {
  const r = http.get(`${BASE}/healthz`);
  check(r, { "status 200": (res) => res.status === 200 });
  try {
    const body = r.json();
    if (body && body.instance) {
      servedByInstance.add(1, { instance: body.instance });
    }
  } catch (_) {}
}

export function userJourney() {
  const username = `user_${__VU}_${__ITER}_${Date.now()}`;
  const password = "secret123";

  const payload = JSON.stringify({ username, password });
  const headers = { "Content-Type": "application/json" };

  const reg = http.post(`${BASE}/auth/register`, payload, { headers });
  check(reg, { "register 201": (r) => r.status === 201 });

  const jar = http.cookieJar();
  jar.clear(BASE);

  const login = http.post(`${BASE}/auth/login`, payload, { headers });
  check(login, {
    "login 200": (r) => r.status === 200,
    "got Set-Cookie": (r) => !!r.headers["Set-Cookie"],
  });

  const seenInstances = new Set();
  for (let i = 0; i < 5; i++) {
    const me = http.get(`${BASE}/auth/me`);
    const ok = check(me, {
      "me 200 (session held across instances)": (r) => r.status === 200,
    });
    if (ok) {
      try {
        const body = me.json();
        if (body && body.served_by) seenInstances.add(body.served_by);
      } catch (_) {}
    }
  }

  const sh = http.post(
    `${BASE}/shorten`,
    JSON.stringify({ url: "https://example.com/" + username }),
    { headers }
  );
  check(sh, { "shorten 201 with session": (r) => r.status === 201 });

  sleep(0.1);
}

export function handleSummary(data) {
  const metric = data.metrics["served_by_instance"];
  let breakdown = "n/a";
  if (metric && metric.values && metric.submetrics) {
    breakdown = Object.entries(metric.submetrics)
      .map(([k, v]) => `  ${k}: ${v.metrics.counter.values.count}`)
      .join("\n");
  }
  return {
    stdout: `
=== Served-by breakdown (healthz) ===
${breakdown}

(Если видишь несколько разных instance — балансировка работает.
 Если auth_journey прошёл с зелёными чеками — Redis-сессии работают
 даже при попадании на разные инстансы.)
`,
  };
}

