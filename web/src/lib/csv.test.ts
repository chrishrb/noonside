import { describe, expect, it } from 'vitest';
import { csvSplit, parseDailyCsv } from './csv';

describe('csv', () => {
  it('splits quoted fields', () => {
    expect(csvSplit('2025-06-01,abc,"WR 1, roof",1.5')).toEqual(['2025-06-01', 'abc', 'WR 1, roof', '1.5']);
  });
  it('parses daily rows with a header and mixed separators', () => {
    const r = parseDailyCsv('inverter,date,energy_wh,pac_max_w\n1,2024-06-01,48210,8120\n2;2024-06-01;47655\n');
    expect(r).toEqual({ rows: [
      { inverter: '1', date: '2024-06-01', energy_wh: 48210, pac_max_w: 8120 },
      { inverter: '2', date: '2024-06-01', energy_wh: 47655 },
    ] });
  });
  it('reports the bad line', () => {
    expect(parseDailyCsv('1,2024-06-01,10\n1,06/02/2024,20')).toEqual({ error: expect.stringContaining('Line 2') });
    expect(parseDailyCsv('   ')).toEqual({ error: 'Add at least one row.' });
  });
});
