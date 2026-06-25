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
out vec2 v_texcoord;
out vec4 v_color;
out float v_tileType;
out float v_seed;
void main() {
  gl_Position = u_projection * vec4(a_position, 0.0, 1.0);
  v_texcoord = a_texcoord;
  v_color = a_color;
  v_tileType = a_tileType;
  v_seed = a_seed;
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
in vec2 v_texcoord;
in vec4 v_color;
in float v_tileType;
in float v_seed;
uniform sampler2D u_texture;
uniform float u_time;
out vec4 fragColor;

// Dave-Hoskins-style 2D hash, returns [0,1].
float hash21(vec2 p) {
  p = fract(p * vec2(123.34, 456.21));
  p += dot(p, p + 45.32);
  return fract(p.x * p.y);
}

void main() {
  vec4 base = texture(u_texture, v_texcoord) * v_color;
  int t = int(v_tileType + 0.5);

  if (t == 0) {
    fragColor = base;
    return;
  }

  // Sample at the tile's pixel grid (TILE_WIDTH = 32 game units per tile).
  // v_texcoord is 0..1 across one tile, so px is in tile-local pixel coords.
  vec2 px = v_texcoord * 32.0;
  vec2 seedOff = vec2(v_seed * 13.37, v_seed * 7.77);
  float n = 0.0; // noise offset in [-1, 1]

  if (t == 2 || t == 3) {
    // Water: horizontal wave bands + grain. Darken wave troughs.
    float band = sin((px.y + v_seed * 17.0) * 0.7 + u_time * 1.6);
    float grain = hash21(floor(vec2(px.x * 0.5, px.y * 0.5)) + seedOff) * 2.0 - 1.0;
    n = band * 0.16 + grain * 0.10;
  } else if (t == 5) {
    // Hill / mountain: chunky vertical grain like rock strata.
    vec2 cell = floor(vec2(px.x * 0.35, px.y * 0.8)) + seedOff;
    float grain = hash21(cell) * 2.0 - 1.0;
    // Occasional darker crack lines every ~10 px
    float crack = step(0.92, hash21(vec2(floor(px.y / 10.0), cell.x)));
    n = grain * 0.20 - crack * 0.18;
  } else if (t == 1 || t == 7) {
    // Road / Bridge: plank lines every 8 px + grain.
    float plankDark = step(0.78, fract(px.y / 8.0));
    vec2 cell = vec2(floor(px.x * 0.4), floor(px.y / 8.0) * 0.5) + seedOff;
    float grain = hash21(cell) * 2.0 - 1.0;
    n = grain * 0.12 - plankDark * 0.16;
  } else if (t == 4) {
    // Forest floor: darker organic noise with occasional light flecks
    // (sunlight through canopy).  Finer grain than plains so tree clusters
    // feel denser.
    vec2 cell = floor(px * 0.8) + seedOff;
    float grain = hash21(cell) * 2.0 - 1.0;
    float fleck = step(0.93, hash21(cell + 3.0));
    n = grain * 0.16 + fleck * 0.14;
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
out vec2 v_texcoord;
out vec4 v_tint;
void main() {
  vec2 pos = a_position * a_spriteSize + a_worldPos;
  gl_Position = u_projection * vec4(pos, 0.0, 1.0);
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
   */
  flush(projectionMatrix, texture, time = 0) {
    if (this.vertexCount === 0) return;

    const gl = this.gl;
    gl.useProgram(this.program);
    gl.uniformMatrix4fv(this.uProjection, false, projectionMatrix);
    gl.activeTexture(gl.TEXTURE0);
    gl.bindTexture(gl.TEXTURE_2D, texture);
    gl.uniform1i(this.uTexture, 0);
    if (this.uTime) gl.uniform1f(this.uTime, time);

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

  /** Draw all queued instances and reset. */
  flush(projectionMatrix, texture, atlasWidth, atlasHeight) {
    if (this.instanceCount === 0) return;

    const gl = this.gl;
    gl.useProgram(this.program);
    gl.uniformMatrix4fv(this.uProjection, false, projectionMatrix);
    gl.uniform2f(this.uAtlasSize, atlasWidth, atlasHeight);
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
  drawUnits(units, camera) {
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
    const barH = 2;         // bar height in pixels
    const barPad = 1;       // gap above unit sprite
    const barMargin = 2;    // inset from unit edges

    for (let i = 0; i < units.length; i++) {
      const u = units[i];
      if (u.hpRatio === undefined || u.hpRatio >= 1.0) continue; // skip full HP

      const ux = u.x - camera.x;
      const uy = u.y - camera.y;
      const bw = u.w - barMargin * 2;
      const bx = ux + barMargin;
      const by = uy - barH - barPad;

      // Background (dark)
      batch.pushColorQuad(bx, by, bw, barH, 0.15, 0.15, 0.15, 0.7);

      // Foreground: green → yellow → red based on hpRatio
      let fr, fg, fb;
      if (u.hpRatio > 0.6) {
        // green to yellow
        const t = (u.hpRatio - 0.6) / 0.4;
        fr = 1.0 - t * 0.8;
        fg = 0.85;
        fb = 0.1;
      } else if (u.hpRatio > 0.3) {
        // yellow to orange
        const t = (u.hpRatio - 0.3) / 0.3;
        fr = 1.0;
        fg = 0.4 + t * 0.45;
        fb = 0.05;
      } else {
        // orange to red
        const t = u.hpRatio / 0.3;
        fr = 0.8 + t * 0.2;
        fg = 0.1 + t * 0.3;
        fb = 0.05;
      }

      const fillW = Math.max(1, bw * u.hpRatio);
      batch.pushColorQuad(bx, by, fillW, barH, fr, fg, fb, 0.85);
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

  /** Flush all batches in the correct render order. */
  endFrame() {
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

    // Pass 1: terrain tiles
    this.terrainBatch.flush(proj, tex, time);

    // Pass 2: terrain objects (already Y-sorted)
    this.objectBatch.flush(proj, tex, time);

    // Pass 2.5: fog overlay (between terrain and units)
    this.fogBatch.flush(proj, tex, time);

    // Pass 3: units (instanced) — uses the dedicated unit atlas texture
    // (set via setUnitTexture) rather than the shared white pixel.  This
    // keeps the unit batch on its own texture binding so terrain/effects
    // passes are unaffected.
    this.unitBatch.flush(proj, this.unitTexture, this.unitAtlasWidth, this.unitAtlasHeight);

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
