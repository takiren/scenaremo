/**
 * `docs/props.schema.json`（唯一の正）と renderer 側 zod スキーマ (`./schema`) の
 * 「形」（プロパティ集合・必須集合）が乖離していないことを CI で検出する（→ issue #25）。
 *
 * Go 側は internal/props/schema_test.go が実際の Build() 出力を docs/props.schema.json で
 * 検証しているので、そちらは既に手当て済み。ここで足りていなかったのは zod 側だけ。
 */
import {readFileSync} from 'node:fs';
import path from 'node:path';
import {fileURLToPath} from 'node:url';
import {describe, expect, test} from 'vitest';
import {z} from 'zod';

import {diffShapes} from './schema.consistency';
import {propsSchema} from './schema';

const dirname = path.dirname(fileURLToPath(import.meta.url));

const loadPropsJsonSchema = () => {
	const raw = readFileSync(path.join(dirname, '../../docs/props.schema.json'), 'utf-8');
	return JSON.parse(raw);
};

describe('diffShapes', () => {
	// 実物どうしを突き合わせる。今この瞬間 docs/props.schema.json と renderer/src/schema.ts の
	// propsSchema は一致しているはずなので、差分は空でなければならない。
	// このテストが唯一の検証だと、常に [] を返すだけの実装でも通ってしまうため、
	// 下の意図的にずらしたケース群が必須。
	test('実物の docs/props.schema.json と propsSchema は一致する', () => {
		const jsonSchema = loadPropsJsonSchema();
		expect(diffShapes(jsonSchema, propsSchema)).toEqual([]);
	});

	test('JSON Schema にあって zod に無いプロパティを検出する', () => {
		const jsonSchema = {
			type: 'object',
			required: ['a'],
			properties: {
				a: {type: 'string'},
				b: {type: 'string'},
			},
		};
		const zodSchema = z.object({a: z.string()});

		const diffs = diffShapes(jsonSchema, zodSchema);
		expect(diffs.length).toBeGreaterThan(0);
		expect(diffs.some((d) => d.includes('b'))).toBe(true);
	});

	test('zod にあって JSON Schema に無いプロパティを検出する', () => {
		const jsonSchema = {
			type: 'object',
			required: ['a'],
			properties: {
				a: {type: 'string'},
			},
		};
		const zodSchema = z.object({a: z.string(), extra: z.string()});

		const diffs = diffShapes(jsonSchema, zodSchema);
		expect(diffs.length).toBeGreaterThan(0);
		expect(diffs.some((d) => d.includes('extra'))).toBe(true);
	});

	test('JSON Schema で必須なのに zod で optional になっているフィールドを検出する', () => {
		const jsonSchema = {
			type: 'object',
			required: ['a'],
			properties: {
				a: {type: 'string'},
			},
		};
		const zodSchema = z.object({a: z.string().optional()});

		const diffs = diffShapes(jsonSchema, zodSchema);
		expect(diffs.length).toBeGreaterThan(0);
		expect(diffs.some((d) => d.includes('a'))).toBe(true);
	});

	test('JSON Schema で optional なのに zod で必須になっているフィールドを検出する', () => {
		const jsonSchema = {
			type: 'object',
			required: [],
			properties: {
				a: {type: 'string'},
			},
		};
		const zodSchema = z.object({a: z.string()});

		const diffs = diffShapes(jsonSchema, zodSchema);
		expect(diffs.length).toBeGreaterThan(0);
		expect(diffs.some((d) => d.includes('a'))).toBe(true);
	});

	test('$ref で参照した $defs の中の乖離も、配列の要素を辿って検出する', () => {
		const jsonSchema = {
			type: 'object',
			required: ['list'],
			properties: {
				list: {
					type: 'array',
					items: {$ref: '#/$defs/item'},
				},
			},
			$defs: {
				item: {
					type: 'object',
					required: ['x'],
					properties: {
						x: {type: 'string'},
					},
				},
			},
		};
		// zod 側は x ではなく y という名前にしてある（ネストしたずれ）。
		const zodSchema = z.object({
			list: z.array(z.object({y: z.string()})),
		});

		const diffs = diffShapes(jsonSchema, zodSchema);
		expect(diffs.length).toBeGreaterThan(0);
		expect(diffs.some((d) => d.includes('x'))).toBe(true);
	});

	test('自由形式オブジェクト（additionalProperties のみで properties が無い）は record/object を受け入れる', () => {
		const jsonSchema = {
			type: 'object',
			required: ['props'],
			properties: {
				props: {type: 'object', additionalProperties: true},
			},
		};
		const zodSchema = z.object({props: z.record(z.string(), z.unknown())});

		expect(diffShapes(jsonSchema, zodSchema)).toEqual([]);
	});

	test('enum フィールドは zod の enum と噛み合っていれば一致とみなす', () => {
		const jsonSchema = {
			type: 'object',
			required: ['aspect'],
			properties: {
				aspect: {enum: ['16:9', '9:16']},
			},
		};
		const zodSchema = z.object({aspect: z.enum(['16:9', '9:16'])});

		expect(diffShapes(jsonSchema, zodSchema)).toEqual([]);
	});
});
