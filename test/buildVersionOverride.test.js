const assert = require('assert');
const fs = require('fs');

describe('Wails build version override', () => {
  it('keeps the HTML placeholder as a value-only replacement', () => {
    const template = fs.readFileSync(
      require.resolve('../desktop/renderer/index.template.html'),
      'utf8'
    );
    const assembler = fs.readFileSync(
      require.resolve('../scripts/assemble-html.js'),
      'utf8'
    );

    assert.match(template, /window\.__JAVFLOW_BUILD_VERSION__\s*=\s*__JAVFLOW_BUILD_VERSION_VALUE__/);
    assert.match(assembler, /replaceAll\('__JAVFLOW_BUILD_VERSION_VALUE__'/);
    assert.doesNotMatch(assembler, /replaceAll\('__JAVFLOW_BUILD_VERSION__'/);
  });
});
