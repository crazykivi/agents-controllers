import { describe, expect, it } from 'vitest'
import { fmtDuration, kindClass, stripAnsi, truncate } from '../format'

describe('truncate', () => {
  it('не трогает короткие строки', () => {
    expect(truncate('hello', 10)).toBe('hello')
  })
  it('обрезает длинные с многоточием', () => {
    expect(truncate('abcdefghij', 4)).toBe('abcd…')
  })
  it('граница ровно n', () => {
    expect(truncate('abcd', 4)).toBe('abcd')
  })
})

describe('fmtDuration', () => {
  it('без старта — прочерк', () => {
    expect(fmtDuration(null, null)).toBe('—')
  })
  it('секунды', () => {
    const s = new Date(Date.UTC(2026, 0, 1, 0, 0, 0)).toISOString()
    const e = new Date(Date.UTC(2026, 0, 1, 0, 0, 42)).toISOString()
    expect(fmtDuration(s, e)).toBe('42s')
  })
  it('минуты и секунды', () => {
    const s = new Date(Date.UTC(2026, 0, 1, 0, 0, 0)).toISOString()
    const e = new Date(Date.UTC(2026, 0, 1, 0, 2, 5)).toISOString()
    expect(fmtDuration(s, e)).toBe('2m 5s')
  })
  it('часы', () => {
    const s = new Date(Date.UTC(2026, 0, 1, 0, 0, 0)).toISOString()
    const e = new Date(Date.UTC(2026, 0, 1, 1, 30, 0)).toISOString()
    expect(fmtDuration(s, e)).toBe('1h 30m')
  })
  it('отрицательная разница не ломает', () => {
    const s = new Date(Date.UTC(2026, 0, 1, 1, 0, 0)).toISOString()
    const e = new Date(Date.UTC(2026, 0, 1, 0, 0, 0)).toISOString()
    expect(fmtDuration(s, e)).toBe('0s')
  })
})

describe('kindClass', () => {
  it('известные виды имеют цвет', () => {
    expect(kindClass('error')).toContain('rose')
    expect(kindClass('thought')).toContain('violet')
  })
  it('неизвестный вид — дефолт', () => {
    expect(kindClass('weird')).toBe('text-slate-300')
  })
})

describe('stripAnsi', () => {
  it('удаляет escape-коды', () => {
    expect(stripAnsi('\u001b[32mgreen\u001b[0m')).toBe('green')
    expect(stripAnsi('a\u001b[?25lb')).toBe('ab')
  })
  it('обычный текст без изменений', () => {
    expect(stripAnsi('plain')).toBe('plain')
  })
})
