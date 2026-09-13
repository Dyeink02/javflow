const assert = require('assert');
const fs = require('fs');
const vm = require('vm');

const viewSource = fs.readFileSync(
  require.resolve('../desktop/renderer/actressAtlasView.js'),
  'utf8'
);
const loadingStyleSource = fs.readFileSync(
  require.resolve('../desktop/renderer/styles/actressatlas-loading.css'),
  'utf8'
);

class FakeClassList {
  constructor(owner) {
    this.owner = owner;
    this.values = new Set();
  }

  add(...names) {
    names.filter(Boolean).forEach((name) => this.values.add(name));
  }

  remove(...names) {
    names.filter(Boolean).forEach((name) => this.values.delete(name));
  }

  toggle(name, force) {
    const enabled = force === undefined ? !this.values.has(name) : Boolean(force);
    if (enabled) this.values.add(name);
    else this.values.delete(name);
    return enabled;
  }

  contains(name) {
    return this.values.has(name);
  }
}

class FakeElement {
  constructor(tagName) {
    this.tagName = tagName;
    this.children = [];
    this.parentElement = null;
    this.dataset = {};
    this.hidden = false;
    this.complete = false;
    this.naturalWidth = 0;
    this.deferDecode = false;
    this.onload = null;
    this.onerror = null;
    this._className = '';
    this.classList = new FakeClassList(this);
  }

  get className() {
    return this._className;
  }

  set className(value) {
    this._className = String(value || '');
    this.classList.values = new Set(this._className.split(/\s+/).filter(Boolean));
  }

  set src(value) {
    this._src = String(value || '');
    this.complete = Boolean(this._src) && !this.deferDecode;
    this.naturalWidth = this.complete ? 100 : 0;
  }

  get src() {
    return this._src || '';
  }

  appendChild(child) {
    child.parentElement = this;
    this.children.push(child);
    return child;
  }

  append(...children) {
    children.forEach((child) => this.appendChild(child));
  }

  insertBefore(child, reference) {
    child.parentElement = this;
    const index = this.children.indexOf(reference);
    if (index < 0) this.children.push(child);
    else this.children.splice(index, 0, child);
    return child;
  }

  replaceWith(replacement) {
    if (!this.parentElement) return;
    const siblings = this.parentElement.children;
    const index = siblings.indexOf(this);
    if (index >= 0) siblings.splice(index, 1, replacement);
    replacement.parentElement = this.parentElement;
  }

  removeAttribute(name) {
    if (name === 'src') this.src = '';
  }

  addEventListener() {}

  querySelector(selector) {
    if (selector.startsWith('.')) {
      const className = selector.slice(1);
      if (this.classList.contains(className)) return this;
    }
    for (const child of this.children) {
      const match = child.querySelector(selector);
      if (match) return match;
    }
    return null;
  }
}

function createHarness() {
  const sandbox = {
    URL,
    location: { href: 'http://localhost/' },
    document: { createElement: (tagName) => new FakeElement(tagName) }
  };
  vm.runInNewContext(viewSource, sandbox, { filename: 'actressAtlasView.js' });
  const image = new FakeElement('img');
  const view = sandbox.desktopActressAtlasView.createActressAtlasView({
    elements: { atlasActorAvatar: image },
    openMedia: () => {}
  });
  return { image, view };
}

describe('actress atlas image settling', () => {
  it('forces a hidden avatar placeholder to stay out of the visual stack', () => {
    assert.match(
      loadingStyleSource,
      /\.atlas-image-placeholder\[hidden\]\s*\{\s*display:\s*none\s*!important;/
    );
  });

  it('hides the avatar loading placeholder when a cached image is already complete', () => {
    const { image, view } = createHarness();

    view.renderSelectedState({
      item: { actressName: 'Test Actress' },
      profile: { avatarUrl: 'data:image/png;base64,AAAA' },
      works: []
    });

    const placeholder = image.parentElement.querySelector('.atlas-image-placeholder');
    assert.strictEqual(image.classList.contains('is-pending'), false);
    assert.strictEqual(image.hidden, false);
    assert.strictEqual(placeholder.hidden, true);
  });

  it('reconciles a reused image binding even when no second load event fires', () => {
    const { image, view } = createHarness();
    const state = {
      item: { actressName: 'Test Actress' },
      profile: { avatarUrl: 'data:image/png;base64,AAAA' },
      works: []
    };

    view.renderSelectedState(state);
    const placeholder = image.parentElement.querySelector('.atlas-image-placeholder');
    image.classList.add('is-pending');
    image.hidden = false;
    placeholder.hidden = false;
    placeholder.classList.add('is-loading');

    view.renderSelectedState(state);

    assert.strictEqual(image.classList.contains('is-pending'), false);
    assert.strictEqual(placeholder.hidden, true);
    assert.strictEqual(placeholder.classList.contains('is-loading'), false);
  });

  it('rechecks a cached image after a detached row is inserted', async () => {
    const { image, view } = createHarness();
    image.deferDecode = true;

    view.renderSelectedState({
      item: { actressName: 'Test Actress' },
      profile: { avatarUrl: 'data:image/png;base64,AAAA' },
      works: []
    });

    const placeholder = image.parentElement.querySelector('.atlas-image-placeholder');
    image.complete = true;
    image.naturalWidth = 100;
    await Promise.resolve();

    assert.strictEqual(image.classList.contains('is-pending'), false);
    assert.strictEqual(placeholder.hidden, true);
  });

  it('does not leave an overlay on an embedded avatar without a load callback', async () => {
    const { image, view } = createHarness();
    image.deferDecode = true;

    view.renderSelectedState({
      item: { actressName: 'Test Actress' },
      profile: { avatarUrl: 'data:image/png;base64,AAAA' },
      works: []
    });

    const placeholder = image.parentElement.querySelector('.atlas-image-placeholder');
    await Promise.resolve();

    assert.strictEqual(image.classList.contains('is-pending'), false);
    assert.strictEqual(placeholder.hidden, true);
  });
});
