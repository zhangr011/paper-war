// client/src/gl.js
// WebGL2 sprite batch renderer for Paper War RTS game.
// 4 batch passes targeting 4-5 draw calls total:
//   1. Terrain tile batch (all visible tiles, 1 draw call)
//   2. Terrain object batch (bridges/trees/buildings, Y-sorted, 1 draw call)
//   3. Unit sprite batch (instanced rendering + texture atlas, 1 draw call)
//   4. Effects layer (ground + air effects, 1-2 draw calls)

// ---------------------------------------------------------------------------
// Shader sources
// ---------------------------------------------------------------------------

const SPRITE_VS = `#version 300 es
in vec2 a_position;
in vec2 a_texcoord;
in vec4 a_color;
in float a_tileType;
in float a_seed;
uniform mat4 u_projection;
// World-pixel camera offset + tile size in screen px (ADR-0026 coastline).
// Used to recover the fragment's integer tile coordinate in the FS so it
// can texelFetch neighbors from the terrain-type texture. Only consulted
// on the Deep-tile coastline path; non-terrain batches early-out first.
uniform vec2 u_camera;
uniform float u_tileSize;
out vec2 v_texcoord;
out vec4 v_color;
out float v_tileType;
out float v_seed;
out vec2 v_worldPos;
void main() {
  gl_Position = u_projection * vec4(a_position, 0.0, 1.0);
  v_texcoord = a_texcoord;
  v_color = a_color;
  v_tileType = a_tileType;
  v_seed = a_seed;
  v_worldPos = a_position + u_camera;
}
`;

// Textured fragment shader.
//   - tileType == 0: flat color (objects, effects, fog, UI)
//   - tileType >= 1: per-pixel hash noise modulates brightness with patterns
//     chosen per terrain type (water waves, mountain grain, road planks, etc.)
//   - Time uniform animates water.
//
// The hash is quantized with floor() so noise falls on a chunky pixel grid,
// giving the pixel-art look of design/map.png instead of smooth gradients.
const SPRITE_FS = `#version 300 es
precision mediump float;
precision highp int;
in vec2 v_texcoord;
in vec4 v_color;
in float v_tileType;
in float v_seed;
in vec2 v_worldPos;
uniform sampler2D u_texture;
uniform float u_time;
// --- ADR-0026 coastline blend ---
// Terrain-type grid uploaded once per match (one R8UI texel per tile).
// The FS samples neighbors with texelFetch to feather Deep↔land seams.
uniform highp usampler2D u_terrainTex;
uniform int u_terrainTexValid;   // 0 = no terrain texture (render flat, no blend)
// Curated blend table — which land types feather against Deep. Data-shaped:
// adding a pair is a JS-array edit, no GLSL change. Bridge (7) deliberately
// absent so Deep↔Bridge stays a hard seam.
uniform int u_blendableLandCount;
uniform int u_blendableLandTypes[8];
uniform vec3 u_deepBlendTarget;  // teal Shallow tint (TERRAIN_COLORS[2])
// Elevation grid (one R8UI texel per tile, hill layer 0/1/2 = low/slope/peak).
// Sampled in the hill branch to make hill tiles render layer-aware (peaks
// rocky + bright, valleys darker/grassy). Uploaded by Renderer.setMapElevationTexture.
uniform highp usampler2D u_elevationTex;
uniform int u_elevationTexValid; // 0 = no elevation texture (uniform hill rendering)
uniform vec2 u_camera;
uniform float u_tileSize;
out vec4 fragColor;

// Dave-Hoskins-style 2D hash, returns [0,1].
float hash21(vec2 p) {
  p = fract(p * vec2(123.34, 456.21));
  p += dot(p, p + 45.32);
  return fract(p.x * p.y);
}

// Returns true if the neighbor terrain type feathers against a Deep tile.
// Data-driven: the list lives in u_blendableLandTypes (set CPU-side).
bool isBlendableLand(int n) {
  for (int i = 0; i < u_blendableLandCount; i++) {
    if (u_blendableLandTypes[i] == n) return true;
  }
  return false;
}

void main() {
  vec4 base = texture(u_texture, v_texcoord) * v_color;
  int t = int(v_tileType + 0.5);

  if (t == 0) {
    fragColor = base;
    return;
  }

  // --- Coastline feathering (ADR-0026) ---
  // Deep tiles only. Non-Deep tiles early-out BEFORE any neighbor fetch, so
  // the common case (plains, forest, hills, roads, walls...) pays nothing.
  // For each of the 4 edges, texelFetch the neighbor; if (Deep, neighbor) is
  // a blendable pair, fade this fragment from deep-blue toward the teal
  // Shallow tint by per-pixel distance-to-edge. Bounds-checked because
  // texelFetch on out-of-range integer textures returns 0 (= Plain), which
  // would otherwise paint a false coastline around the map border.
  if (t == 3 && u_terrainTexValid == 1) {
    vec2 tilePos = v_worldPos / u_tileSize;
    vec2 frac = fract(tilePos);             // per-axis distance from top-left edge, 0..1
    ivec2 tc = ivec2(floor(tilePos));       // this fragment's integer tile coord
    ivec2 dim = textureSize(u_terrainTex, 0);

    // Teal band width as a fraction of one tile (~0.4 of the half-tile).
    const float COAST = 0.2;
    // Per-edge distance, normalized to [0, 1] over COAST width, then clamped.
    float dL = frac.x;          // distance to left edge
    float dR = 1.0 - frac.x;    // distance to right edge
    float dT = frac.y;          // distance to top edge
    float dB = 1.0 - frac.y;    // distance to bottom edge
    float coast = 0.0;

    ivec2 np = tc + ivec2(-1, 0);
    if (np.x >= 0 && np.y >= 0 && np.x < dim.x && np.y < dim.y &&
        isBlendableLand(int(texelFetch(u_terrainTex, np, 0).x))) {
      coast = max(coast, 1.0 - dL / COAST);
    }
    np = tc + ivec2(1, 0);
    if (np.x >= 0 && np.y >= 0 && np.x < dim.x && np.y < dim.y &&
        isBlendableLand(int(texelFetch(u_terrainTex, np, 0).x))) {
      coast = max(coast, 1.0 - dR / COAST);
    }
    np = tc + ivec2(0, -1);
    if (np.x >= 0 && np.y >= 0 && np.x < dim.x && np.y < dim.y &&
        isBlendableLand(int(texelFetch(u_terrainTex, np, 0).x))) {
      coast = max(coast, 1.0 - dT / COAST);
    }
    np = tc + ivec2(0, 1);
    if (np.x >= 0 && np.y >= 0 && np.x < dim.x && np.y < dim.y &&
        isBlendableLand(int(texelFetch(u_terrainTex, np, 0).x))) {
      coast = max(coast, 1.0 - dB / COAST);
    }

    coast = clamp(coast, 0.0, 1.0);
    base.rgb = mix(base.rgb, u_deepBlendTarget, coast);
    // Waterline foam — a pale band right at the shore (where the teal blend
    // is strongest), so the water→land transition reads as a real shoreline
    // rather than a flat gradient.
    float foam = smoothstep(0.55, 1.0, coast);
    base.rgb = mix(base.rgb, vec3(0.62, 0.70, 0.66), foam * 0.45);
  }

  // Sample at the tile's pixel grid (TILE_WIDTH = 32 game units per tile).
  // v_texcoord is 0..1 across one tile, so px is in tile-local pixel coords.
  vec2 px = v_texcoord * 32.0;
  vec2 seedOff = vec2(v_seed * 13.37, v_seed * 7.77);
  float n = 0.0; // noise offset in [-1, 1]

  if (t == 2 || t == 3) {
    // Water: two overlapping wave bands (horizontal + diagonal) for richer
    // ripple motion, plus fine grain.
    float band = sin((px.y + v_seed * 17.0) * 0.7 + u_time * 1.6);
    float band2 = sin(dot(px, vec2(0.6, 0.4)) + u_time * 1.1 + v_seed * 5.0);
    float grain = hash21(floor(vec2(px.x * 0.5, px.y * 0.5)) + seedOff) * 2.0 - 1.0;
    n = band * 0.14 + band2 * 0.06 + grain * 0.10;
    // Animated specular sparkles — sun-glint on the surface. Time is
    // quantized so sparkles twinkle on/off (re-rolled 3×/sec) rather than
    // glow; only ~3.5% of sub-cells light up. Tunable via the threshold.
    float twink = floor(u_time * 3.0);
    float sparkle = step(0.965, hash21(floor(vec2(px.x * 0.5, px.y * 0.5)) + vec2(twink, v_seed)));
    n += sparkle * 0.22;
  } else if (t == 5) {
    // Hill / mountain: chunky vertical grain like rock strata.
    vec2 cell = floor(vec2(px.x * 0.35, px.y * 0.8)) + seedOff;
    float grain = hash21(cell) * 2.0 - 1.0;
    // Occasional darker crack lines every ~10 px
    float crack = step(0.92, hash21(vec2(floor(px.y / 10.0), cell.x)));
    // Directional relief — light from the top-left: brighten the top/left
    // edge of the tile, darken bottom/right. Stacks on the CPU-side
    // hillShadeRGB layer tint to fake raised 3D terrain.
    float relief = (0.5 - v_texcoord.y) * 0.12 + (0.5 - v_texcoord.x) * 0.06;
    n = grain * 0.20 - crack * 0.18 + relief;
    // Elevation-aware: sample this tile's hill layer (0 valley, 1 slope,
    // 2 peak) and render each distinctly. Bounds-checked because
    // texelFetch off-range returns 0 (= valley).
    if (u_elevationTexValid == 1) {
      vec2 tilePosE = v_worldPos / u_tileSize;
      ivec2 tcE = ivec2(floor(tilePosE));
      ivec2 dimE = textureSize(u_elevationTex, 0);
      int layer = 0;
      if (tcE.x >= 0 && tcE.y >= 0 && tcE.x < dimE.x && tcE.y < dimE.y) {
        layer = int(texelFetch(u_elevationTex, tcE, 0).x);
      }
      if (layer == 2) {
        // Peak: brightest sun-catch, heavier rock cracks, faint pale
        // dusting on the upper face so summits read as exposed stone/snow.
        n += 0.10;
        n -= step(0.70, hash21(cell + 5.0)) * 0.12;
        n += (1.0 - v_texcoord.y) * 0.06;
      } else if (layer == 0) {
        // Valley: deeper shade, sparser cracks (grassy low ground).
        n -= 0.07;
        n += crack * 0.12;
      }
      // layer 1 (slope): base relief only — the slope face catches the light.
    }
  } else if (t == 7) {
    // Bridge: plank lines every 8 px + grain.
    float plankDark = step(0.78, fract(px.y / 8.0));
    vec2 cell = vec2(floor(px.x * 0.4), floor(px.y / 8.0) * 0.5) + seedOff;
    float grain = hash21(cell) * 2.0 - 1.0;
    n = grain * 0.12 - plankDark * 0.16;
  } else if (t == 1) {
    // Road: warm worn-dirt — fine grain plus two faint darkened ruts at
    // 1/3 and 2/3 across the tile (a well-traveled track), distinct from
    // the wooden plank look of bridges.
    vec2 cell = floor(vec2(px.x * 0.5, px.y * 0.5)) + seedOff;
    float grain = hash21(cell) * 2.0 - 1.0;
    float rut = smoothstep(0.05, 0.0, abs(v_texcoord.x - 0.33)) +
                smoothstep(0.05, 0.0, abs(v_texcoord.x - 0.66));
    n = grain * 0.10 - rut * 0.09;
  } else if (t == 4) {
    // Forest floor: darker organic noise with occasional light flecks
    // (sunlight through canopy).  Finer grain than plains so tree clusters
    // feel denser.
    vec2 cell = floor(px * 0.8) + seedOff;
    float grain = hash21(cell) * 2.0 - 1.0;
    float fleck = step(0.93, hash21(cell + 3.0));
    // Dappled sunlight — larger, softer bright patches (lower frequency
    // than the flecks) reading as light filtering through the canopy.
    vec2 dappleCell = floor(px * 0.25) + seedOff;
    float dapple = step(0.78, hash21(dappleCell + 9.0));
    n = grain * 0.16 + fleck * 0.14 + dapple * 0.10;
  } else if (t == 0) {
    // Plains: fine scattered-grass clumps.  A small-scale hash gives the
    // impression of individual grass tufts; the per-tile patchwork
    // brightness (set CPU-side in buildTerrainTiles) provides the broader
    // light/dark field variation.  Keeping this branch's amplitude modest
    // so the patchwork pattern stays readable.
    vec2 cell = floor(px * 0.9) + seedOff;
    float grain = hash21(cell) * 2.0 - 1.0;
    // Sparse brighter grass blades (~12% of pixels) for subtle highlights.
    float blade = step(0.88, hash21(cell + 7.0));
    n = grain * 0.13 + blade * 0.08;
  } else {
    // Swamp / desert / generic: organic per-pixel grain.
    vec2 cell = floor(px * 0.7) + seedOff;
    float grain = hash21(cell) * 2.0 - 1.0;
    n = grain * 0.15;
  }

  fragColor = vec4(clamp(base.rgb * (1.0 + n), 0.0, 1.0), base.a);
}
`;

const INSTANCED_VS = `#version 300 es
in vec2 a_position;      // quad vertices (unit quad 0..1)
in vec2 a_texcoord;      // base texcoords (0..1)
in vec2 a_worldPos;      // per-instance: world position (screen coords)
in vec2 a_spriteOffset;  // per-instance: atlas offset (pixels)
in vec2 a_spriteSize;    // per-instance: sprite size (pixels)
in vec4 a_tint;          // per-instance: color tint
uniform mat4 u_projection;
uniform vec2 u_atlasSize; // atlas dimensions for texcoord normalization
uniform float u_zoom;     // camera zoom — scales the on-screen quad without
                          // affecting atlas sampling (decoupled from a_spriteSize)
uniform float u_unitScale; // visual scale multiplier for units (1.5 = 50% larger).
                          // Multiplies the on-screen quad but NOT atlas sampling.
out vec2 v_texcoord;
out vec4 v_tint;
void main() {
  // Vertex position: scale the unit quad by spriteSize, zoom, AND unitScale
  // so the rendered quad grows proportionally. The centering offset (for
  // unitScale != 1.0) is handled on the CPU side in buildUnitDescriptors.
  vec2 pos = a_position * a_spriteSize * u_zoom * u_unitScale + a_worldPos;
  gl_Position = u_projection * vec4(pos, 0.0, 1.0);
  // Texcoord: use spriteSize ONLY (no zoom). Sampling a 32×32 cell of the
  // atlas regardless of on-screen size — this is the decoupling that lets
  // units scale visually without going out of atlas bounds.
  v_texcoord = (a_texcoord * a_spriteSize + a_spriteOffset) / u_atlasSize;
  v_tint = a_tint;
}
`;

const INSTANCED_FS = `#version 300 es
precision mediump float;
in vec2 v_texcoord;
in vec4 v_tint;
uniform sampler2D u_texture;
out vec4 fragColor;
void main() {
  fragColor = texture(u_texture, v_texcoord) * v_tint;
}
`;

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

function compileShader(gl, type, source) {
  const shader = gl.createShader(type);
  gl.shaderSource(shader, source);
  gl.compileShader(shader);
  if (!gl.getShaderParameter(shader, gl.COMPILE_STATUS)) {
    const info = gl.getShaderInfoLog(shader);
    gl.deleteShader(shader);
    throw new Error(`Shader compile error: ${info}`);
  }
  return shader;
}

function createProgram(gl, vsSource, fsSource) {
  const vs = compileShader(gl, gl.VERTEX_SHADER, vsSource);
  const fs = compileShader(gl, gl.FRAGMENT_SHADER, fsSource);
  const program = gl.createProgram();
  gl.attachShader(program, vs);
  gl.attachShader(program, fs);
  gl.linkProgram(program);
  if (!gl.getProgramParameter(program, gl.LINK_STATUS)) {
    const info = gl.getProgramInfoLog(program);
    gl.deleteProgram(program);
    throw new Error(`Program link error: ${info}`);
  }
  gl.deleteShader(vs);
  gl.deleteShader(fs);
  return program;
}

/**
 * Build an orthographic projection matrix (column-major Float32Array[16]).
 * Y-down coordinate system: (0,0) at top-left, (width,height) at bottom-right.
 */
function ortho(width, height) {
  // prettier-ignore
  return new Float32Array([
    2 / width,  0,           0, 0,
    0,          -2 / height,  0, 0,
    0,          0,           -1, 0,
    -1,         1,            0, 1,
  ]);
}

/**
 * Create a 1x1 white pixel texture used as a placeholder until a real
 * texture atlas is loaded.  Makes every draw render as a colored quad.
 */
function createWhitePixelTexture(gl) {
  const tex = gl.createTexture();
  gl.bindTexture(gl.TEXTURE_2D, tex);
  gl.texImage2D(
    gl.TEXTURE_2D,
    0,
    gl.RGBA,
    1,
    1,
    0,
    gl.RGBA,
    gl.UNSIGNED_BYTE,
    new Uint8Array([255, 255, 255, 255]),
  );
  gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_MIN_FILTER, gl.NEAREST);
  gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_MAG_FILTER, gl.NEAREST);
  gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_WRAP_S, gl.CLAMP_TO_EDGE);
  gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_WRAP_T, gl.CLAMP_TO_EDGE);
  return tex;
}

// ---------------------------------------------------------------------------
// SpriteBatch  --  batched quads for terrain tiles & objects
// ---------------------------------------------------------------------------

// Each vertex: x, y, u, v, r, g, b, a, tileType, seed  = 10 floats
//   - tileType: terrain type id; 0 means "flat color" (no noise), >=1 enables
//     the texture branch of the fragment shader with type-specific patterns.
//   - seed: per-tile deterministic seed so identical terrain types don't share
//     the same noise pattern.
const VERTEX_FLOATS = 10;
const VERTEX_BYTES = VERTEX_FLOATS * 4;
const MAX_BATCH_VERTICES = 60000; // ~15000 quads before flush
const QUAD_VERTICES = 6; // two triangles

// Curated Deep↔land blend table (ADR-0026). Adding a pair = push to this
// array; no shader edit needed. Bridge (7) intentionally excluded so
// Deep↔Bridge keeps a hard seam (bridges read as structures, not shores).
const BLENDABLE_LAND_TYPES = [
  0,   // Plain
  4,   // Forest
  5,   // Hill
  17,  // Brush
];

// Teal Shallow tint used as the coastline fade target (TERRAIN_COLORS[2]).
const DEEP_BLEND_TARGET = [0.22, 0.40, 0.55];

class SpriteBatch {
  /**
   * @param {WebGL2RenderingContext} gl
   * @param {WebGLProgram} program
   */
  constructor(gl, program) {
    this.gl = gl;
    this.program = program;
    this.vertexCount = 0;

    // CPU-side buffer (grows as needed up to MAX_BATCH_VERTICES)
    this.buffer = new Float32Array(MAX_BATCH_VERTICES * VERTEX_FLOATS);

    // Attribute locations
    this.aPosition = gl.getAttribLocation(program, 'a_position');
    this.aTexcoord = gl.getAttribLocation(program, 'a_texcoord');
    this.aColor = gl.getAttribLocation(program, 'a_color');
    this.aTileType = gl.getAttribLocation(program, 'a_tileType');
    this.aSeed = gl.getAttribLocation(program, 'a_seed');

    // Uniform locations
    this.uProjection = gl.getUniformLocation(program, 'u_projection');
    this.uTexture = gl.getUniformLocation(program, 'u_texture');
    this.uTime = gl.getUniformLocation(program, 'u_time');
    // ADR-0026 coastline uniforms. Shared program — set once at construction.
    // Non-terrain batches early-out before any of these are consulted.
    this.uCamera = gl.getUniformLocation(program, 'u_camera');
    this.uTileSize = gl.getUniformLocation(program, 'u_tileSize');
    this.uTerrainTex = gl.getUniformLocation(program, 'u_terrainTex');
    this.uTerrainTexValid = gl.getUniformLocation(program, 'u_terrainTexValid');
    this.uElevationTex = gl.getUniformLocation(program, 'u_elevationTex');
    this.uElevationTexValid = gl.getUniformLocation(program, 'u_elevationTexValid');
    this.uBlendableLandCount = gl.getUniformLocation(program, 'u_blendableLandCount');
    this.uBlendableLandTypes = gl.getUniformLocation(program, 'u_blendableLandTypes[0]');
    this.uDeepBlendTarget = gl.getUniformLocation(program, 'u_deepBlendTarget');

    // One-time upload of the curated blend table + target tint. The terrain
    // texture itself (and its validity flag) is set by Renderer.setMapTerrainTexture.
    gl.useProgram(program);
    if (this.uTerrainTex) gl.uniform1i(this.uTerrainTex, 1); // bind to unit 1
    if (this.uElevationTex) gl.uniform1i(this.uElevationTex, 2); // bind to unit 2
    if (this.uBlendableLandCount) {
      gl.uniform1i(this.uBlendableLandCount, BLENDABLE_LAND_TYPES.length);
    }
    if (this.uBlendableLandTypes) {
      gl.uniform1iv(this.uBlendableLandTypes, BLENDABLE_LAND_TYPES);
    }
    if (this.uDeepBlendTarget) {
      gl.uniform3fv(this.uDeepBlendTarget, DEEP_BLEND_TARGET);
    }
    if (this.uTerrainTexValid) gl.uniform1i(this.uTerrainTexValid, 0);
    if (this.uElevationTexValid) gl.uniform1i(this.uElevationTexValid, 0);
    gl.useProgram(null);

    // VAO + VBO
    this.vao = gl.createVertexArray();
    this.vbo = gl.createBuffer();

    gl.bindVertexArray(this.vao);
    gl.bindBuffer(gl.ARRAY_BUFFER, this.vbo);
    gl.bufferData(
      gl.ARRAY_BUFFER,
      MAX_BATCH_VERTICES * VERTEX_BYTES,
      gl.DYNAMIC_DRAW,
    );

    // a_position  (offset 0, 2 floats)
    gl.enableVertexAttribArray(this.aPosition);
    gl.vertexAttribPointer(this.aPosition, 2, gl.FLOAT, false, VERTEX_BYTES, 0);
    // a_texcoord  (offset 8, 2 floats)
    gl.enableVertexAttribArray(this.aTexcoord);
    gl.vertexAttribPointer(this.aTexcoord, 2, gl.FLOAT, false, VERTEX_BYTES, 8);
    // a_color     (offset 16, 4 floats)
    gl.enableVertexAttribArray(this.aColor);
    gl.vertexAttribPointer(this.aColor, 4, gl.FLOAT, false, VERTEX_BYTES, 16);
    // a_tileType  (offset 32, 1 float)
    gl.enableVertexAttribArray(this.aTileType);
    gl.vertexAttribPointer(this.aTileType, 1, gl.FLOAT, false, VERTEX_BYTES, 32);
    // a_seed      (offset 36, 1 float)
    gl.enableVertexAttribArray(this.aSeed);
    gl.vertexAttribPointer(this.aSeed, 1, gl.FLOAT, false, VERTEX_BYTES, 36);

    gl.bindVertexArray(null);
  }

  /** Reset batch state for a new frame. */
  reset() {
    this.vertexCount = 0;
  }

  /**
   * Push a single quad (two triangles) into the batch with full control over
   * texture coordinates and (optionally) tile-type/seed for textured terrain.
   *
   * @param {number} x      top-left x (screen pixels)
   * @param {number} y      top-left y (screen pixels)
   * @param {number} w      width
   * @param {number} h      height
   * @param {number} u0     texcoord left
   * @param {number} v0     texcoord top
   * @param {number} u1     texcoord right
   * @param {number} v1     texcoord bottom
   * @param {number} r      red   0..1
   * @param {number} g      green 0..1
   * @param {number} b      blue  0..1
   * @param {number} a      alpha 0..1
   * @param {number} [tileType=0]  0=flat color, >=1 enables per-pixel noise
   * @param {number} [seed=0]      per-tile deterministic noise seed
   */
  pushQuad(x, y, w, h, u0, v0, u1, v1, r, g, b, a, tileType = 0, seed = 0) {
    // NOTE: this mid-batch flush path lacks projection/texture args. In
    // practice the terrain batch never overflows MAX_BATCH_VERTICES (~15k
    // quads) so this branch is unreachable; the public flush() at endFrame
    // always supplies the args. Kept for safety.
    if (this.vertexCount + QUAD_VERTICES > MAX_BATCH_VERTICES) {
      this.flush();
    }

    const x1 = x + w;
    const y1 = y + h;
    const o = this.vertexCount * VERTEX_FLOATS;
    const buf = this.buffer;

    // Triangle 1: top-left, bottom-left, bottom-right
    buf[o]      = x;  buf[o + 1]  = y;  buf[o + 2]  = u0; buf[o + 3]  = v0;
    buf[o + 4]  = r;  buf[o + 5]  = g;  buf[o + 6]  = b;  buf[o + 7]  = a;
    buf[o + 8]  = tileType; buf[o + 9] = seed;

    buf[o + 10] = x;  buf[o + 11] = y1; buf[o + 12] = u0; buf[o + 13] = v1;
    buf[o + 14] = r;  buf[o + 15] = g;  buf[o + 16] = b;  buf[o + 17] = a;
    buf[o + 18] = tileType; buf[o + 19] = seed;

    buf[o + 20] = x1; buf[o + 21] = y1; buf[o + 22] = u1; buf[o + 23] = v1;
    buf[o + 24] = r;  buf[o + 25] = g;  buf[o + 26] = b;  buf[o + 27] = a;
    buf[o + 28] = tileType; buf[o + 29] = seed;

    // Triangle 2: top-left, bottom-right, top-right
    buf[o + 30] = x;  buf[o + 31] = y;  buf[o + 32] = u0; buf[o + 33] = v0;
    buf[o + 34] = r;  buf[o + 35] = g;  buf[o + 36] = b;  buf[o + 37] = a;
    buf[o + 38] = tileType; buf[o + 39] = seed;

    buf[o + 40] = x1; buf[o + 41] = y1; buf[o + 42] = u1; buf[o + 43] = v1;
    buf[o + 44] = r;  buf[o + 45] = g;  buf[o + 46] = b;  buf[o + 47] = a;
    buf[o + 48] = tileType; buf[o + 49] = seed;

    buf[o + 50] = x1; buf[o + 51] = y;  buf[o + 52] = u1; buf[o + 53] = v1;
    buf[o + 54] = r;  buf[o + 55] = g;  buf[o + 56] = b;  buf[o + 57] = a;
    buf[o + 58] = tileType; buf[o + 59] = seed;

    this.vertexCount += QUAD_VERTICES;
  }

  /**
   * Push a flat-colored quad (uses full texture, so with the 1x1 white texture
   * it renders as a flat color rectangle).  tileType defaults to 0 which the
   * fragment shader treats as "no texture", preserving existing behavior for
   * all non-terrain callers (objects, effects, fog, UI).
   */
  pushColorQuad(x, y, w, h, r, g, b, a) {
    this.pushQuad(x, y, w, h, 0, 0, 1, 1, r, g, b, a, 0, 0);
  }

  /**
   * Push a textured terrain quad.  Same as pushColorQuad but with tileType and
   * seed passed through so the fragment shader applies the appropriate noise
   * pattern.  Used by drawTerrain for textured pixel-art tiles.
   */
  pushTexturedQuad(x, y, w, h, r, g, b, tileType, seed) {
    this.pushQuad(x, y, w, h, 0, 0, 1, 1, r, g, b, 1.0, tileType, seed);
  }

  /**
   * Upload and draw all queued vertices, then reset.
   * @param {Float32Array} projectionMatrix
   * @param {WebGLTexture} texture
   * @param {number} [time]  current time in seconds (drives water animation)
   * @param {{cameraX:number, cameraY:number, tileSize:number}|null} [terrainUniforms]
   *        Optional world-camera + tile-size for the ADR-0026 coastline path.
   *        Only meaningful for the terrain batch; non-terrain batches ignore it.
   */
  flush(projectionMatrix, texture, time = 0, terrainUniforms = null) {
    if (this.vertexCount === 0) return;

    const gl = this.gl;
    gl.useProgram(this.program);
    gl.uniformMatrix4fv(this.uProjection, false, projectionMatrix);
    gl.activeTexture(gl.TEXTURE0);
    gl.bindTexture(gl.TEXTURE_2D, texture);
    gl.uniform1i(this.uTexture, 0);
    if (this.uTime) gl.uniform1f(this.uTime, time);
    // Coastline path inputs. The uniforms persist (program state) so setting
    // them only on the terrain flush is enough — other batches early-out
    // before consulting them.
    if (terrainUniforms) {
      if (this.uCamera) gl.uniform2f(this.uCamera, terrainUniforms.cameraX, terrainUniforms.cameraY);
      if (this.uTileSize) gl.uniform1f(this.uTileSize, terrainUniforms.tileSize);
    }

    const byteLen = this.vertexCount * VERTEX_BYTES;
    gl.bindVertexArray(this.vao);
    gl.bindBuffer(gl.ARRAY_BUFFER, this.vbo);
    gl.bufferSubData(gl.ARRAY_BUFFER, 0, this.buffer.subarray(0, this.vertexCount * VERTEX_FLOATS));

    gl.drawArrays(gl.TRIANGLES, 0, this.vertexCount);
    gl.bindVertexArray(null);

    this.vertexCount = 0;
  }
}

// ---------------------------------------------------------------------------
// InstancedBatch  --  instanced quads for units
// ---------------------------------------------------------------------------

// Per-vertex data (shared unit quad): x, y, u, v  = 4 floats
// Per-instance data: worldX, worldY, spriteOffX, spriteOffY, spriteW, spriteH, tintR, tintG, tintB, tintA = 10 floats

const INSTANCE_FLOATS = 10;

class InstancedBatch {
  /**
   * @param {WebGL2RenderingContext} gl
   * @param {WebGLProgram} program
   * @param {number} maxInstances  maximum instances per flush
   */
  constructor(gl, program, maxInstances = 10000) {
    this.gl = gl;
    this.program = program;
    this.maxInstances = maxInstances;
    this.instanceCount = 0;

    // CPU-side instance buffer
    this.instanceBuffer = new Float32Array(maxInstances * INSTANCE_FLOATS);

    // Attribute locations
    this.aPosition = gl.getAttribLocation(program, 'a_position');
    this.aTexcoord = gl.getAttribLocation(program, 'a_texcoord');
    this.aWorldPos = gl.getAttribLocation(program, 'a_worldPos');
    this.aSpriteOffset = gl.getAttribLocation(program, 'a_spriteOffset');
    this.aSpriteSize = gl.getAttribLocation(program, 'a_spriteSize');
    this.aTint = gl.getAttribLocation(program, 'a_tint');

    // Uniform locations
    this.uProjection = gl.getUniformLocation(program, 'u_projection');
    this.uTexture = gl.getUniformLocation(program, 'u_texture');
    this.uAtlasSize = gl.getUniformLocation(program, 'u_atlasSize');
    this.uZoom = gl.getUniformLocation(program, 'u_zoom');
    this.uUnitScale = gl.getUniformLocation(program, 'u_unitScale');

    // Unit quad vertices (position + texcoord): a fullscreen quad 0..1
    const quadVerts = new Float32Array([
      // x    y    u    v
      0, 0,   0, 0,
      0, 1,   0, 1,
      1, 1,   1, 1,
      0, 0,   0, 0,
      1, 1,   1, 1,
      1, 0,   1, 0,
    ]);

    // VAO
    this.vao = gl.createVertexArray();
    gl.bindVertexArray(this.vao);

    // Quad VBO (static, shared by all instances)
    this.quadVBO = gl.createBuffer();
    gl.bindBuffer(gl.ARRAY_BUFFER, this.quadVBO);
    gl.bufferData(gl.ARRAY_BUFFER, quadVerts, gl.STATIC_DRAW);

    const QUAD_STRIDE = 4 * 4; // 4 floats * 4 bytes
    gl.enableVertexAttribArray(this.aPosition);
    gl.vertexAttribPointer(this.aPosition, 2, gl.FLOAT, false, QUAD_STRIDE, 0);
    gl.enableVertexAttribArray(this.aTexcoord);
    gl.vertexAttribPointer(this.aTexcoord, 2, gl.FLOAT, false, QUAD_STRIDE, 8);

    // Instance VBO (dynamic, per-instance data)
    this.instanceVBO = gl.createBuffer();
    gl.bindBuffer(gl.ARRAY_BUFFER, this.instanceVBO);
    gl.bufferData(
      gl.ARRAY_BUFFER,
      maxInstances * INSTANCE_FLOATS * 4,
      gl.DYNAMIC_DRAW,
    );

    const INSTANCE_STRIDE = INSTANCE_FLOATS * 4; // 10 floats * 4 bytes

    // a_worldPos (2 floats, per instance)
    gl.enableVertexAttribArray(this.aWorldPos);
    gl.vertexAttribPointer(this.aWorldPos, 2, gl.FLOAT, false, INSTANCE_STRIDE, 0);
    gl.vertexAttribDivisor(this.aWorldPos, 1);

    // a_spriteOffset (2 floats, per instance)
    gl.enableVertexAttribArray(this.aSpriteOffset);
    gl.vertexAttribPointer(this.aSpriteOffset, 2, gl.FLOAT, false, INSTANCE_STRIDE, 8);
    gl.vertexAttribDivisor(this.aSpriteOffset, 1);

    // a_spriteSize (2 floats, per instance)
    gl.enableVertexAttribArray(this.aSpriteSize);
    gl.vertexAttribPointer(this.aSpriteSize, 2, gl.FLOAT, false, INSTANCE_STRIDE, 16);
    gl.vertexAttribDivisor(this.aSpriteSize, 1);

    // a_tint (4 floats, per instance)
    gl.enableVertexAttribArray(this.aTint);
    gl.vertexAttribPointer(this.aTint, 4, gl.FLOAT, false, INSTANCE_STRIDE, 24);
    gl.vertexAttribDivisor(this.aTint, 1);

    gl.bindVertexArray(null);
  }

  /** Reset for a new frame. */
  reset() {
    this.instanceCount = 0;
  }

  /**
   * Push one instance.
   * @param {number} worldX       screen x position
   * @param {number} worldY       screen y position
   * @param {number} spriteOffX   atlas offset x (pixels)
   * @param {number} spriteOffY   atlas offset y (pixels)
   * @param {number} spriteW      sprite width (pixels)
   * @param {number} spriteH      sprite height (pixels)
   * @param {number} r            tint red   0..1
   * @param {number} g            tint green 0..1
   * @param {number} b            tint blue  0..1
   * @param {number} a            tint alpha 0..1
   */
  pushInstance(worldX, worldY, spriteOffX, spriteOffY, spriteW, spriteH, r, g, b, a) {
    if (this.instanceCount >= this.maxInstances) {
      return false; // caller should flush first
    }
    const o = this.instanceCount * INSTANCE_FLOATS;
    const buf = this.instanceBuffer;
    buf[o]     = worldX;
    buf[o + 1] = worldY;
    buf[o + 2] = spriteOffX;
    buf[o + 3] = spriteOffY;
    buf[o + 4] = spriteW;
    buf[o + 5] = spriteH;
    buf[o + 6] = r;
    buf[o + 7] = g;
    buf[o + 8] = b;
    buf[o + 9] = a;
    this.instanceCount++;
    return true;
  }

  /** Draw all queued instances and reset.
   *
   *  @param {Float32Array} projectionMatrix  orthographic projection
   *  @param {WebGLTexture} texture           atlas texture to sample
   *  @param {number} atlasWidth              atlas width in pixels (for texcoord normalization)
   *  @param {number} atlasHeight             atlas height in pixels
   *  @param {number} [zoom=1]                camera zoom factor.
   *  @param {number} [unitScale=1]           per-unit visual scale (1.5 = 150%).
   */
  flush(projectionMatrix, texture, atlasWidth, atlasHeight, zoom = 1, unitScale = 1) {
    if (this.instanceCount === 0) return;

    const gl = this.gl;
    gl.useProgram(this.program);
    gl.uniformMatrix4fv(this.uProjection, false, projectionMatrix);
    gl.uniform2f(this.uAtlasSize, atlasWidth, atlasHeight);
    if (this.uZoom) gl.uniform1f(this.uZoom, zoom);
    if (this.uUnitScale) gl.uniform1f(this.uUnitScale, unitScale);
    gl.activeTexture(gl.TEXTURE0);
    gl.bindTexture(gl.TEXTURE_2D, texture);
    gl.uniform1i(this.uTexture, 0);

    const byteLen = this.instanceCount * INSTANCE_FLOATS * 4;
    gl.bindVertexArray(this.vao);
    gl.bindBuffer(gl.ARRAY_BUFFER, this.instanceVBO);
    gl.bufferSubData(gl.ARRAY_BUFFER, 0, this.instanceBuffer.subarray(0, this.instanceCount * INSTANCE_FLOATS));

    gl.drawArraysInstanced(gl.TRIANGLES, 0, QUAD_VERTICES, this.instanceCount);
    gl.bindVertexArray(null);

    this.instanceCount = 0;
  }
}

// ---------------------------------------------------------------------------
// Renderer  --  main entry point
// ---------------------------------------------------------------------------

export class Renderer {
  /**
   * @param {HTMLCanvasElement} canvas
   */
  constructor(canvas) {
    const gl = canvas.getContext('webgl2', {
      alpha: false,
      antialias: false,
      premultipliedAlpha: false,
    });
    if (!gl) throw new Error('WebGL2 not supported');

    this.gl = gl;
    this.canvas = canvas;

    // Shader programs
    const spriteProgram = createProgram(gl, SPRITE_VS, SPRITE_FS);
    const instancedProgram = createProgram(gl, INSTANCED_VS, INSTANCED_FS);

    // Batches
    this.terrainBatch = new SpriteBatch(gl, spriteProgram);
    this.objectBatch = new SpriteBatch(gl, spriteProgram);
    this.fogBatch = new SpriteBatch(gl, spriteProgram);
    this.effectsBatch = new SpriteBatch(gl, spriteProgram);
    this.unitBatch = new InstancedBatch(gl, instancedProgram);

    // Placeholder texture: 1x1 white pixel. Used by terrain, object,
    // fog, and effects batches — those rely on the fragment-shader
    // noise/colour path, not on a real atlas.
    this.whiteTexture = createWhitePixelTexture(gl);

    // Default atlas size (will be updated when real atlas is loaded)
    this.atlasWidth = 1;
    this.atlasHeight = 1;

    // Unit sprite atlas.  Populated by setUnitTexture() with a canvas
    // drawn procedurally at startup (see unit_atlas.js).  Until that
    // call lands, the unit pass falls back to the white pixel so
    // rendering still works (every unit is a flat tinted quad).
    this.unitTexture = this.whiteTexture;
    this.unitAtlasWidth = 1;
    this.unitAtlasHeight = 1;

    // GL state
    gl.enable(gl.BLEND);
    gl.blendFunc(gl.SRC_ALPHA, gl.ONE_MINUS_SRC_ALPHA);
  }

  /** Resize canvas to match its CSS layout size at the current DPR. */
  resize() {
    this.dpr = window.devicePixelRatio || 1;
    const rect = this.canvas.getBoundingClientRect();
    this.canvas.width = Math.floor(rect.width * this.dpr);
    this.canvas.height = Math.floor(rect.height * this.dpr);
    this.gl.viewport(0, 0, this.canvas.width, this.canvas.height);
  }

  /**
   * Upload a procedurally-generated canvas (or any image source) as the
   * unit sprite atlas texture.  Called once at startup after the unit
   * atlas is drawn (see unit_atlas.js).  Uses NEAREST filtering to
   * preserve pixel-art edges.
   * @param {HTMLCanvasElement|ImageBitmap|HTMLImageElement} canvas
   * @param {number} atlasW    atlas width  in pixels
   * @param {number} atlasH    atlas height in pixels
   */
  setUnitTexture(canvas, atlasW, atlasH) {
    const gl = this.gl;
    // Dispose the previous texture if we allocated one (skip the shared
    // white pixel — it's still in use by the other batches).
    if (this.unitTexture && this.unitTexture !== this.whiteTexture) {
      gl.deleteTexture(this.unitTexture);
    }
    const tex = gl.createTexture();
    gl.bindTexture(gl.TEXTURE_2D, tex);
    gl.pixelStorei(gl.UNPACK_FLIP_Y_WEBGL, false);
    gl.texImage2D(
      gl.TEXTURE_2D,
      0,
      gl.RGBA,
      gl.RGBA,
      gl.UNSIGNED_BYTE,
      canvas,
    );
    gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_MIN_FILTER, gl.NEAREST);
    gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_MAG_FILTER, gl.NEAREST);
    gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_WRAP_S, gl.CLAMP_TO_EDGE);
    gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_WRAP_T, gl.CLAMP_TO_EDGE);
    this.unitTexture = tex;
    this.unitAtlasWidth = atlasW;
    this.unitAtlasHeight = atlasH;
  }

  /**
   * Upload the terrain-type grid as a single-channel R8UI texture
   * (ADR-0026). One texel per tile; the FS samples neighbors with
   * texelFetch to feather Deep↔land coastlines. Terrain is static for a
   * match, so upload once and leave bound to texture unit 1.
   *
   * WebGL2 guarantees integer textures; R8UI + usampler2D is the clean
   * choice (no packed encoding/decode needed). Degrade gracefully: if this
   * is never called, u_terrainTexValid stays 0 and the shader renders flat
   * tiles exactly as before.
   *
   * @param {Uint8Array} terrainData  flat [ty*mw+tx] terrain-type grid
   * @param {number} mapW
   * @param {number} mapH
   */
  setMapTerrainTexture(terrainData, mapW, mapH) {
    const gl = this.gl;
    if (this.terrainTypeTexture) gl.deleteTexture(this.terrainTypeTexture);
    const tex = gl.createTexture();
    gl.activeTexture(gl.TEXTURE1);
    gl.bindTexture(gl.TEXTURE_2D, tex);
    gl.pixelStorei(gl.UNPACK_FLIP_Y_WEBGL, false);
    gl.texImage2D(
      gl.TEXTURE_2D,
      0,
      gl.R8UI,
      mapW,
      mapH,
      0,
      gl.RED_INTEGER,
      gl.UNSIGNED_BYTE,
      terrainData,
    );
    // Integer textures require NEAREST filtering; CLAMP_TO_EDGE for wrap.
    gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_MIN_FILTER, gl.NEAREST);
    gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_MAG_FILTER, gl.NEAREST);
    gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_WRAP_S, gl.CLAMP_TO_EDGE);
    gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_WRAP_T, gl.CLAMP_TO_EDGE);
    this.terrainTypeTexture = tex;
    this.terrainMapW = mapW;
    this.terrainMapH = mapH;

    // Flip the valid flag on the shared sprite program. Setting it through
    // any one SpriteBatch's uniform location is sufficient — they share
    // the program and thus the uniform state.
    const loc = this.terrainBatch.uTerrainTexValid;
    if (loc) {
      gl.useProgram(this.terrainBatch.program);
      gl.uniform1i(loc, 1);
      gl.useProgram(null);
    }
    // Restore active texture to unit 0 so subsequent white-pixel binds land
    // on the right unit (SpriteBatch.flush binds to TEXTURE0 explicitly,
    // but be defensive).
    gl.activeTexture(gl.TEXTURE0);
  }

  /**
   * Upload the per-tile elevation grid (hill layer 0/1/2) as an R8UI texture
   * on TEXTURE2, and flip u_elevationTexValid so the hill shader branch can
   * sample it for layer-aware rendering. Mirror of setMapTerrainTexture.
   */
  setMapElevationTexture(elevationData, mapW, mapH) {
    const gl = this.gl;
    if (this.elevationTexture) gl.deleteTexture(this.elevationTexture);
    const tex = gl.createTexture();
    gl.activeTexture(gl.TEXTURE2);
    gl.bindTexture(gl.TEXTURE_2D, tex);
    gl.pixelStorei(gl.UNPACK_FLIP_Y_WEBGL, false);
    gl.texImage2D(
      gl.TEXTURE_2D, 0, gl.R8UI, mapW, mapH, 0,
      gl.RED_INTEGER, gl.UNSIGNED_BYTE, elevationData,
    );
    gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_MIN_FILTER, gl.NEAREST);
    gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_MAG_FILTER, gl.NEAREST);
    gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_WRAP_S, gl.CLAMP_TO_EDGE);
    gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_WRAP_T, gl.CLAMP_TO_EDGE);
    this.elevationTexture = tex;
    const loc = this.terrainBatch.uElevationTexValid;
    if (loc) {
      gl.useProgram(this.terrainBatch.program);
      gl.uniform1i(loc, 1);
      gl.useProgram(null);
    }
    gl.activeTexture(gl.TEXTURE0);
  }

  /**
   * Mark the terrain-type texture as absent (e.g. on disconnect / map
   * unload). Shader falls back to flat-tile rendering, no coastline.
   */
  clearMapTerrainTexture() {
    const gl = this.gl;
    if (this.terrainTypeTexture) {
      gl.deleteTexture(this.terrainTypeTexture);
      this.terrainTypeTexture = null;
    }
    if (this.elevationTexture) {
      gl.deleteTexture(this.elevationTexture);
      this.elevationTexture = null;
    }
    const loc = this.terrainBatch.uTerrainTexValid;
    if (loc) {
      gl.useProgram(this.terrainBatch.program);
      gl.uniform1i(loc, 0);
      gl.useProgram(null);
    }
    const eloc = this.terrainBatch.uElevationTexValid;
    if (eloc) {
      gl.useProgram(this.terrainBatch.program);
      gl.uniform1i(eloc, 0);
      gl.useProgram(null);
    }
  }

  /** Clear screen and reset all batch state. Call once per frame. */
  beginFrame() {
    const gl = this.gl;
    // Dark void color matching design/map.png border (~#141414). Slightly
    // warmer than pure black so the map edge reads as a framed border rather
    // than a clipped canvas.
    gl.clearColor(0.055, 0.06, 0.05, 1.0);
    gl.clear(gl.COLOR_BUFFER_BIT);

    this.terrainBatch.reset();
    this.objectBatch.reset();
    this.fogBatch.reset();
    this.unitBatch.reset();
    this.effectsBatch.reset();
  }

  /**
   * Set the camera zoom factor for the next endFrame() flush.
   * The instanced unit shader uses this to scale unit quads to match
   * terrain tile scaling (issue #45 follow-up: units weren't tracking
   * camera zoom, appearing disproportionately small when zoomed in and
   * huge when zoomed out).
   * @param {number} zoom
   */
  setZoom(zoom) {
    this.currentZoom = zoom;
  }

  setUnitScale(scale) {
    this.unitScale = scale;
  }

  // -----------------------------------------------------------------------
  // Batch population methods
  // The actual game data (tiles, objects, units, effects) will come from
  // state.js.  For now these accept simple descriptor arrays.
  // -----------------------------------------------------------------------

  /**
   * Batch terrain tiles visible in the viewport.
   * @param {Array<{x:number, y:number, w:number, h:number, r:number, g:number, b:number, [tileType]:number, [seed]:number}>} tiles
   * @param {{ x:number, y:number }} camera  camera offset (screen pixels)
   */
  drawTerrain(tiles, camera) {
    const batch = this.terrainBatch;
    for (let i = 0; i < tiles.length; i++) {
      const t = tiles[i];
      // If the tile carries tileType/seed, use the textured path so the
      // fragment shader applies per-pixel noise.  Otherwise fall back to a
      // flat color quad (legacy / fallback path).
      if (t.tileType && t.tileType > 0) {
        batch.pushTexturedQuad(
          t.x - camera.x,
          t.y - camera.y,
          t.w,
          t.h,
          t.r,
          t.g,
          t.b,
          t.tileType,
          t.seed || 0,
        );
      } else {
        batch.pushColorQuad(
          t.x - camera.x,
          t.y - camera.y,
          t.w,
          t.h,
          t.r,
          t.g,
          t.b,
          1.0,
        );
      }
    }
  }

  /**
   * Batch terrain objects (bridges, trees, buildings).  Objects should be
   * Y-sorted before calling for correct painter's algorithm ordering.
   * @param {Array<{x:number, y:number, w:number, h:number, r:number, g:number, b:number, sortY:number}>} objects
   * @param {{ x:number, y:number }} camera
   */
  drawObjects(objects, camera) {
    // Sort by Y (painter's algorithm: draw far first)
    objects.sort((a, b) => a.sortY - b.sortY);

    const batch = this.objectBatch;
    for (let i = 0; i < objects.length; i++) {
      const o = objects[i];
      batch.pushColorQuad(
        o.x - camera.x,
        o.y - camera.y,
        o.w,
        o.h,
        o.r,
        o.g,
        o.b,
        1.0,
      );
    }
  }

  /**
   * Batch unit sprites via instanced rendering.
   *
   * Each descriptor may carry atlas coordinates (`spriteOffsetX/Y`,
   * `spriteW/H` in atlas pixels).  When present, they select a sub-rect
   * of the unit texture atlas.  When absent, the call falls back to
   * (0, 0, w, h) — sampling the whole texture, which is the legacy
   * flat-quad path used before the atlas was wired up.
   *
   * @param {Array<{x:number, y:number, w:number, h:number, r:number, g:number, b:number,
   *                [spriteOffsetX]:number, [spriteOffsetY]:number,
   *                [spriteW]:number, [spriteH]:number}>} units
   * @param {{ x:number, y:number }} camera
   */
  drawUnits(units, camera, zoom = 1) {
    // NOTE: zoom is forwarded to unitBatch.flush via the renderer's
    // render() loop. This method only pushes instances; the zoom uniform
    // is applied at flush time. Kept in the signature so callers know
    // the rendered size depends on zoom (the position is already
    // pre-scaled by the caller via TILE_WIDTH*zoom in world-pixel space).
    const batch = this.unitBatch;
    for (let i = 0; i < units.length; i++) {
      const u = units[i];
      // Atlas source rect.  Defaults to (0,0)→(w,h) which (with the
      // 1×1 white fallback texture) renders the legacy tinted quad.
      const sox = u.spriteOffsetX || 0;
      const soy = u.spriteOffsetY || 0;
      const sw  = u.spriteW !== undefined ? u.spriteW : u.w;
      const sh  = u.spriteH !== undefined ? u.spriteH : u.h;
      batch.pushInstance(
        u.x - camera.x,
        u.y - camera.y,
        sox, soy,         // atlas offset (px)
        sw, sh,           // atlas sprite size (px)
        u.r, u.g, u.b, 1.0,
      );
    }
  }

  /**
   * Draw HP bars above units using the effects batch (color quads).
   * Each bar is a thin rectangle: background (dark) + foreground (green/yellow/red).
   * @param {Array<{x:number, y:number, w:number, hpRatio:number}>} units
   * @param {{ x:number, y:number }} camera
   */
  drawHPBars(units, camera) {
    const batch = this.effectsBatch;
    const CMD_BAR_H = 6;      // commander bar height (doubled from 3)
    const REG_BAR_H = 4;      // regular bar = 2/3 of commander (no segments)
    const barPad = 1;
    const barMargin = 2;
    const now = performance.now();

    for (let i = 0; i < units.length; i++) {
      const u = units[i];
      // Concealment badge (ADR-0029): a small foliage leaf at the unit's
      // top-right marks own units hidden in Forest/Brush. Drawn before the
      // full-HP skip so the stealth state stays legible even at 100% HP.
      if (u.concealed) {
        const cx = u.x - camera.x;
        const cy = u.y - camera.y;
        const leaf = Math.max(3, Math.round(4 * (this.currentZoom || 1)));
        const lx = cx + u.w - leaf - 1;
        const ly = cy - leaf - 5;
        batch.pushColorQuad(lx - 1, ly - 1, leaf + 2, leaf + 2, 0.10, 0.30, 0.08, 0.6);
        batch.pushColorQuad(lx, ly, leaf, leaf, 0.22, 0.55, 0.18, 0.95);
        batch.pushColorQuad(lx + 1, ly + 1, leaf - 2, leaf - 2, 0.35, 0.66, 0.24, 0.95);
      }
      if (u.hpRatio === undefined) continue;
      if (u.hpRatio >= 0.99 && !u.isCommander) continue;

      const barH = u.isCommander ? CMD_BAR_H : REG_BAR_H;
      const ux = u.x - camera.x;
      const uy = u.y - camera.y;
      // Commander bar = full sprite width; regular units = 2/3 width,
      // centered on the sprite.
      const fullW = u.w - barMargin * 2;
      const bw = u.isCommander ? fullW : Math.floor(fullW * 2 / 3);
      const bx = u.isCommander ? ux + barMargin : ux + (u.w - bw) / 2;
      const by = uy - barH - barPad;

      // Foreground color: green → yellow → red based on hpRatio.
      let fr, fg, fb;
      if (u.hpRatio > 0.6) {
        const t = (u.hpRatio - 0.6) / 0.4;
        fr = 1.0 - t * 0.8; fg = 0.85; fb = 0.1;
      } else if (u.hpRatio > 0.3) {
        const t = (u.hpRatio - 0.3) / 0.3;
        fr = 1.0; fg = 0.4 + t * 0.45; fb = 0.05;
      } else {
        const t = u.hpRatio / 0.3;
        fr = 0.8 + t * 0.2; fg = 0.1 + t * 0.3; fb = 0.05;
      }

      // Low-HP urgency pulse.
      let fillAlpha = 0.92;
      if (u.hpRatio < 0.3) {
        fillAlpha = 0.55 + 0.37 * Math.sin(now / 180 + i * 0.7);
      }

      // Glow halo.
      batch.pushColorQuad(bx - 2, by - 1, bw + 4, barH + 2, fr * 0.3, fg * 0.3, fb * 0.3, 0.25);

      // Border.
      batch.pushColorQuad(bx - 1, by - 1, bw + 2, barH + 2, 0.05, 0.05, 0.05, 0.85);

      // Background track.
      batch.pushColorQuad(bx, by, bw, barH, 0.10, 0.10, 0.10, 0.9);

      // Fill — two-tone bevel.
      const fillW = Math.max(1, bw * u.hpRatio);
      const halfH = Math.floor(barH / 2);
      batch.pushColorQuad(bx, by, fillW, halfH, Math.min(1, fr * 1.15), Math.min(1, fg * 1.15), Math.min(1, fb * 1.15), fillAlpha);
      batch.pushColorQuad(bx, by + halfH, fillW, barH - halfH, fr * 0.82, fg * 0.82, fb * 0.82, fillAlpha);

      // Segmented pips — commanders only: 5 segments (dividers at 20/40/60/80%).
      if (u.isCommander) {
        for (let p = 1; p <= 4; p++) {
          const px = bx + bw * p / 5;
          batch.pushColorQuad(px - 0.5, by, 1, barH, 0, 0, 0, 0.45);
        }
      }

      // Damage flash.
      if (u.damageFlash > 0.05) {
        batch.pushColorQuad(bx, by, fillW, barH, 1, 1, 1, u.damageFlash * 0.6);
      }

      // Commander gold pip.
      if (u.isCommander && fillW > 2) {
        batch.pushColorQuad(bx + fillW - 1, by, 1, barH, 0.95, 0.78, 0.2, 0.95);
      }
    }
  }

  /**
   * Batch effects (ground and air).
   * @param {Array<{x:number, y:number, w:number, h:number, r:number, g:number, b:number, a:number}>} effects
   * @param {{ x:number, y:number }} camera
   */
  drawEffects(effects, camera) {
    const batch = this.effectsBatch;
    for (let i = 0; i < effects.length; i++) {
      const e = effects[i];
      batch.pushColorQuad(
        e.x - camera.x,
        e.y - camera.y,
        e.w,
        e.h,
        e.r,
        e.g,
        e.b,
        e.a !== undefined ? e.a : 1.0,
      );
    }
  }

  /**
   * Render active particles from a ParticleSystem pool (issue #37).
   * Particles carry world-tile coords; we convert to screen pixels with
   * TILE_WIDTH×zoom and center the size-scaled quad. Alpha is derived
   * from age/life so particles fade out. This iterates the pool
   * directly (no descriptor allocation) and pushes into the effects
   * batch, which is flushed in endFrame().
   *
   * @param {object} particles  ParticleSystem instance
   * @param {number} zoom       current camera zoom factor
   * @param {{x:number, y:number}} camera  camera offset in screen px
   */
  drawParticles(particles, zoom, camera) {
    const batch = this.effectsBatch;
    const tilePx = 32 * zoom; // TILE_WIDTH=32
    particles.forEachActive((p) => {
      const t = p.age / p.life;
      const alpha = p.baseAlpha * (1 - t);
      const screenX = p.x * tilePx - camera.x;
      const screenY = p.y * tilePx - camera.y;
      const size = p.size * zoom;
      batch.pushColorQuad(
        screenX - size / 2,
        screenY - size / 2,
        size, size,
        p.r, p.g, p.b,
        alpha,
      );
    });
  }

  /**
   * Batch fog overlay quads.
   * @param {Array<{x:number, y:number, w:number, h:number, r:number, g:number, b:number, a:number}>} fogTiles
   * @param {{ x:number, y:number }} camera
   */
  drawFog(fogTiles, camera) {
    const batch = this.fogBatch;
    for (let i = 0; i < fogTiles.length; i++) {
      const t = fogTiles[i];
      batch.pushColorQuad(
        t.x - camera.x,
        t.y - camera.y,
        t.w,
        t.h,
        t.r, t.g, t.b, t.a
      );
    }
  }

  /**
   * Flush all batches in the correct render order.
   * @param {{cameraX:number, cameraY:number, tileSize:number}|null} [terrainUniforms]
   *        World-camera offset + tile size in screen px. Forwarded only to
   *        the terrain batch for the ADR-0026 coastline path.
   */
  endFrame(terrainUniforms = null) {
    // Use CSS pixel dimensions so all game coords are in CSS pixels
    const cssW = this.canvas.width / (this.dpr || 1);
    const cssH = this.canvas.height / (this.dpr || 1);
    const proj = ortho(cssW, cssH);
    const tex = this.whiteTexture;
    const aw = this.atlasWidth;
    const ah = this.atlasHeight;
    // Time in seconds — drives water wave animation and other time-varying
    // effects in the textured fragment shader.
    const time = performance.now() / 1000;

    // Pass 1: terrain tiles (coastline uniforms forwarded only here)
    this.terrainBatch.flush(proj, tex, time, terrainUniforms);

    // Pass 2: terrain objects (already Y-sorted)
    this.objectBatch.flush(proj, tex, time);

    // Pass 2.5: fog overlay (between terrain and units)
    this.fogBatch.flush(proj, tex, time);

    // Pass 3: units (instanced) — uses the dedicated unit atlas texture
    // (set via setUnitTexture) rather than the shared white pixel.  This
    // keeps the unit batch on its own texture binding so terrain/effects
    // passes are unaffected.
    //
    // v1.2.1: pass the camera zoom so the instanced shader can scale
    // unit quads to match terrain tiles. Before this, units rendered at
    // a fixed 32×32 pixels regardless of zoom level, so zooming in made
    // units look tiny relative to the map and zooming out made them huge.
    this.unitBatch.flush(proj, this.unitTexture, this.unitAtlasWidth, this.unitAtlasHeight, this.currentZoom || 1, this.unitScale || 1);

    // Pass 4: effects
    this.effectsBatch.flush(proj, tex, time);
  }

  // -----------------------------------------------------------------------
  // Texture atlas support (for future use when art assets are available)
  // -----------------------------------------------------------------------

  /**
   * Set the texture atlas used for sprite rendering.
   * @param {WebGLTexture} texture
   * @param {number} width
   * @param {number} height
   */
  setAtlas(texture, width, height) {
    this.whiteTexture = texture;
    this.atlasWidth = width;
    this.atlasHeight = height;
  }
}
