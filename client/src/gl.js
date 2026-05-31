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
uniform mat4 u_projection;
out vec2 v_texcoord;
out vec4 v_color;
void main() {
  gl_Position = u_projection * vec4(a_position, 0.0, 1.0);
  v_texcoord = a_texcoord;
  v_color = a_color;
}
`;

const SPRITE_FS = `#version 300 es
precision mediump float;
in vec2 v_texcoord;
in vec4 v_color;
uniform sampler2D u_texture;
out vec4 fragColor;
void main() {
  fragColor = texture(u_texture, v_texcoord) * v_color;
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

// Each vertex: x, y, u, v, r, g, b, a  = 8 floats
const VERTEX_FLOATS = 8;
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

    // Uniform locations
    this.uProjection = gl.getUniformLocation(program, 'u_projection');
    this.uTexture = gl.getUniformLocation(program, 'u_texture');

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

    gl.bindVertexArray(null);
  }

  /** Reset batch state for a new frame. */
  reset() {
    this.vertexCount = 0;
  }

  /**
   * Push a single quad (two triangles) into the batch.
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
   */
  pushQuad(x, y, w, h, u0, v0, u1, v1, r, g, b, a) {
    // Flush if we would exceed the buffer
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

    buf[o + 8]  = x;  buf[o + 9]  = y1; buf[o + 10] = u0; buf[o + 11] = v1;
    buf[o + 12] = r;  buf[o + 13] = g;  buf[o + 14] = b;  buf[o + 15] = a;

    buf[o + 16] = x1; buf[o + 17] = y1; buf[o + 18] = u1; buf[o + 19] = v1;
    buf[o + 20] = r;  buf[o + 21] = g;  buf[o + 22] = b;  buf[o + 23] = a;

    // Triangle 2: top-left, bottom-right, top-right
    buf[o + 24] = x;  buf[o + 25] = y;  buf[o + 26] = u0; buf[o + 27] = v0;
    buf[o + 28] = r;  buf[o + 29] = g;  buf[o + 30] = b;  buf[o + 31] = a;

    buf[o + 32] = x1; buf[o + 33] = y1; buf[o + 34] = u1; buf[o + 35] = v1;
    buf[o + 36] = r;  buf[o + 37] = g;  buf[o + 38] = b;  buf[o + 39] = a;

    buf[o + 40] = x1; buf[o + 41] = y;  buf[o + 42] = u1; buf[o + 43] = v1;
    buf[o + 44] = r;  buf[o + 45] = g;  buf[o + 46] = b;  buf[o + 47] = a;

    this.vertexCount += QUAD_VERTICES;
  }

  /**
   * Push a colored quad (uses full texture, so with the 1x1 white texture it
   * renders as a flat color rectangle).
   */
  pushColorQuad(x, y, w, h, r, g, b, a) {
    this.pushQuad(x, y, w, h, 0, 0, 1, 1, r, g, b, a);
  }

  /** Upload and draw all queued vertices, then reset. */
  flush(projectionMatrix, texture) {
    if (this.vertexCount === 0) return;

    const gl = this.gl;
    gl.useProgram(this.program);
    gl.uniformMatrix4fv(this.uProjection, false, projectionMatrix);
    gl.activeTexture(gl.TEXTURE0);
    gl.bindTexture(gl.TEXTURE_2D, texture);
    gl.uniform1i(this.uTexture, 0);

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

    // Placeholder texture: 1x1 white pixel
    this.whiteTexture = createWhitePixelTexture(gl);

    // Default atlas size (will be updated when real atlas is loaded)
    this.atlasWidth = 1;
    this.atlasHeight = 1;

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

  /** Clear screen and reset all batch state. Call once per frame. */
  beginFrame() {
    const gl = this.gl;
    gl.clearColor(0.1, 0.1, 0.12, 1.0);
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
   * @param {Array<{x:number, y:number, w:number, h:number, r:number, g:number, b:number}>} tiles
   * @param {{ x:number, y:number }} camera  camera offset (screen pixels)
   */
  drawTerrain(tiles, camera) {
    const batch = this.terrainBatch;
    for (let i = 0; i < tiles.length; i++) {
      const t = tiles[i];
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
   * @param {Array<{x:number, y:number, w:number, h:number, r:number, g:number, b:number}>} units
   * @param {{ x:number, y:number }} camera
   */
  drawUnits(units, camera) {
    const batch = this.unitBatch;
    for (let i = 0; i < units.length; i++) {
      const u = units[i];
      batch.pushInstance(
        u.x - camera.x,
        u.y - camera.y,
        0, 0,             // sprite offset (placeholder)
        u.w, u.h,         // sprite size
        u.r, u.g, u.b, 1.0,
      );
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

    // Pass 1: terrain tiles
    this.terrainBatch.flush(proj, tex);

    // Pass 2: terrain objects (already Y-sorted)
    this.objectBatch.flush(proj, tex);

    // Pass 2.5: fog overlay (between terrain and units)
    this.fogBatch.flush(proj, tex);

    // Pass 3: units (instanced)
    this.unitBatch.flush(proj, tex, aw, ah);

    // Pass 4: effects
    this.effectsBatch.flush(proj, tex);
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
