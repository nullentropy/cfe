package main

// The CRT shaders stay in caution's shared ES3 subset so the browser and native
// terminals render identically, and stay hue-neutral so the tube never fights
// the theme.
//
// crtCurve is the barrel amount; GIBSON MODE reads it to bend picks the way the
// tube bends the image, so keep it in sync with the 0.012 literals below.
const crtCurve = 0.012

// crtPass is the tube switched off: a passthrough that keeps the effect layer
// mounted, so toggling never has to add or remove the prop.
const crtPass = `
vec4 effect(vec2 uv) { return src(uv); }`

// crtStatic has no u_time, so when the tube is on but idle the frame never
// changes: one cached composite quad, no ongoing cost.
const crtStatic = `
vec4 effect(vec2 uv) {
  vec2 c = uv * 2.0 - 1.0;
  c *= 1.0 + 0.012 * dot(c, c);            // gentle: clicks must still land where they look
  vec2 suv = (c + 1.0) * 0.5;
  vec4 col = src(suv);
  float ab = 0.0022 * dot(c, c);
  col.r = src(suv + vec2(ab, 0.0)).r;
  col.b = src(suv - vec2(ab, 0.0)).b;
  float scan = 0.955 + 0.045 * sin(suv.y * u_res.y * 3.14159);
  col.rgb *= scan * 1.10;
  col.rgb *= 1.0 - 0.09 * dot(c, c);
  return col;
}`

// crtFlicker is the animated tube: the static look plus hum and an occasional
// glitch burst. crtStatic stays glitch-free so idle costs nothing; this is only
// mounted when flicker is on.
const crtFlicker = `
float gh(float x) { return fract(sin(x * 127.1) * 43758.5453); }
vec4 effect(vec2 uv) {
  vec2 c = uv * 2.0 - 1.0;
  c *= 1.0 + 0.012 * dot(c, c);
  vec2 suv = (c + 1.0) * 0.5;

  float slot = floor(u_time * 3.0);
  float burst = smoothstep(0.93, 0.99, gh(slot));
  float bnd = floor(suv.y * 42.0 + slot * 3.0);
  // step() gates ~20% of bands, so a burst is a few slivers tearing, not the whole screen doubling
  float tear = (gh(bnd) - 0.5) * 0.04 * burst * step(0.80, gh(bnd + 7.0));
  suv.x = fract(suv.x + tear);
  float ab = 0.0022 * dot(c, c) + 0.0012 * burst;

  vec4 col = src(suv);
  col.r = src(suv + vec2(ab, 0.0)).r;
  col.b = src(suv - vec2(ab, 0.0)).b;

  float scan = 0.955 + 0.045 * sin(suv.y * u_res.y * 3.14159);
  col.rgb *= scan * 1.10;
  col.rgb *= 1.0 - 0.09 * dot(c, c);
  float flick = 0.992 + 0.008 * sin(u_time * 73.0) * (0.6 + 0.4 * sin(u_time * 13.7));
  float band = exp(-24.0 * abs(fract(suv.y * 0.7 - u_time * 0.05) - 0.5));
  col.rgb *= flick;
  col.rgb += col.rgb * 0.07 * band;

  float drop = smoothstep(0.005, 0.0, abs(suv.y - gh(slot + 3.0))) * burst;
  col.rgb += drop * 0.28;
  return col;
}`

// gibsonFrag is the sidebar live feed: a raymarched city with the camera flying
// an endless street, tinted by the identity. Small pane, so ~96 steps is fine.
const gibsonFrag = `
float hash21(vec2 p) { return fract(sin(dot(p, vec2(127.1, 311.7))) * 43758.5453123); }

float sdBox(vec3 p, vec3 b) {
  vec3 d = abs(p) - b;
  return length(max(d, vec3(0.0))) + min(max(d.x, max(d.y, d.z)), 0.0);
}

float towerH(vec2 cell) { float h = hash21(cell); return 0.6 + 6.0 * h * h; }

float map(vec3 p) {
  vec2 cell = floor(p.xz / 4.0);
  // flat where the camera's street runs, plus the occasional empty lot
  if ((cell.x > -1.5 && cell.x < 0.5) || hash21(cell + 31.0) < 0.12) return p.y;
  vec2 l = fract(p.xz / 4.0) * 4.0 - 2.0;
  float h = towerH(cell);
  float w = 0.55 + 0.65 * hash21(cell + 17.0);
  float tower = sdBox(vec3(l.x, p.y - h, l.y), vec3(w, h, w));
  return min(tower, p.y);
}

vec4 effect(vec2 uv) {
  vec2 sc = uv * 2.0 - 1.0;
  sc.x *= u_res.x / u_res.y;
  sc.y = -sc.y;
  float T = u_time * 3.0;
  vec3 ro = vec3(1.4 * sin(u_time * 0.22), 3.6, T);
  vec3 rd = normalize(vec3(sc.x, sc.y * 0.9 - 0.30, 1.5));
  vec3 tint = vec3(u_cr, u_cg, u_cb);

  float t = 0.0;
  bool hit = false;
  vec3 p = ro;
  for (int i = 0; i < 96; i++) {
    p = ro + rd * t;
    float d = map(p);
    if (d < 0.002 * t + 0.001) { hit = true; break; }
    // Cap the step: the per-cell SDF is blind to neighbors, and an uncapped
    // step tunnels through tall towers at grazing angles.
    t += min(d, 1.4) * 0.7;
    if (t > 70.0) break;
  }

  float horizon = pow(max(0.0, 1.0 - abs(rd.y) * 4.0), 3.0);
  vec3 col = tint * (0.015 + 0.20 * horizon);

  if (hit) {
    vec2 cell = floor(p.xz / 4.0);
    vec2 l = fract(p.xz / 4.0) * 4.0 - 2.0;
    vec3 m;
    if (p.y < 0.03) {
      float b = min(2.0 - abs(l.x), 2.0 - abs(l.y));
      float g = smoothstep(0.14, 0.0, b);
      m = tint * (0.02 + 1.3 * g);
    } else {
      float h = towerH(cell);
      float u = (abs(l.x) > abs(l.y)) ? l.y : l.x;
      float lit  = step(0.45, hash21(vec2(floor(u * 3.0) + cell.x * 7.0, floor(p.y * 2.2) + cell.y * 13.0)));
      float grid = step(0.35, fract(u * 3.0)) * step(0.30, fract(p.y * 2.2));
      float rim  = smoothstep(0.30, 0.0, abs(2.0 * h - p.y));
      m = tint * (0.03 + 0.85 * lit * grid + 1.4 * rim);
    }
    float fog = exp(-t * 0.055);
    col = mix(col, m, fog);
  }
  return vec4(col, 1.0);
}`
