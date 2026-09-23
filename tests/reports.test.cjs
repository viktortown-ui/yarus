'use strict';
const test = require('node:test');
const assert = require('node:assert/strict');

require('../web/reports.js');

function state() {
  return {
    space: { name: 'Тест', currency: 'RUB' },
    items: {
      a: { id: 'a', name: 'Гайка', sku: 'A-1', unit: 'шт', min: 2000, price: 350, archived: false },
      b: { id: 'b', name: 'Архив', sku: 'B-1', unit: 'шт', min: 0, price: 100, archived: true },
    },
    places: { p1: { id: 'p1', name: 'Полка 1' }, p2: { id: 'p2', name: 'Полка 2' } },
    stocks: {
      one: { item: 'a', place: 'p1', qty: 1250 },
      two: { item: 'a', place: 'p2', qty: 2000 },
      old: { item: 'b', place: 'p1', qty: 9000 },
    },
  };
}

test('stock report summary aggregates places and skips archived items', () => {
  const rows = YarusReports.rowsFor(state(), { layout: 'summary' });
  assert.deepEqual(rows.map(row => row.name), ['Гайка']);
  assert.equal(rows[0].quantity, '3,25 шт');
  assert.equal(rows[0].minimum, '2 шт');
  assert.equal(rows[0].value, 1138n);
});

test('detailed report respects place and item filters', () => {
  const rows = YarusReports.rowsFor(state(), { layout: 'places', placeId: 'p2', itemIds: ['a'] });
  assert.equal(rows.length, 1);
  assert.equal(rows[0].place, 'Полка 2');
  assert.equal(rows[0].quantity, '2 шт');
});

test('very large stock values use exact integer arithmetic', () => {
  const value = state();
  value.items.a.price = 10_000_000_000_000;
  value.stocks.one.qty = 1_000_000_000_000;
  value.stocks.two.qty = 0;
  const rows = YarusReports.rowsFor(value, { layout: 'summary' });
  assert.equal(rows[0].value, 10_000_000_000_000_000_000_000n);
});

test('PDF encoder produces an ASCII PDF with one image page', () => {
  const pdf = YarusReports.pdfFromImages([{ width: 1, height: 1, jpeg: '\xff\xd8\xff\xd9' }]);
  assert.ok(pdf.startsWith('%PDF-1.4'));
  assert.match(pdf, /\/Count 1/);
  assert.ok(pdf.endsWith('%%EOF\n'));
});
