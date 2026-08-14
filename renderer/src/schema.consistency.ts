import type { ZodType } from 'zod';

export type JsonSchemaNode = {
	type?: string;
	enum?: unknown[];
	properties?: Record<string, JsonSchemaNode>;
	required?: string[];
	items?: JsonSchemaNode;
	$ref?: string;
	additionalProperties?: boolean | JsonSchemaNode;
	[key: string]: unknown;
};

export type JsonSchemaDoc = JsonSchemaNode & {
	$defs?: Record<string, JsonSchemaNode>;
};

export function diffShapes(jsonSchemaDoc: JsonSchemaDoc, zodSchema: ZodType): string[] {
	const diffs: string[] = [];

	function compare(path: string, jsonNode: JsonSchemaNode, zodNode: any) {
		// 1. Resolve $ref
		if (jsonNode.$ref) {
			const prefix = '#/$defs/';
			if (jsonNode.$ref.startsWith(prefix)) {
				const defName = jsonNode.$ref.slice(prefix.length);
				const resolved = jsonSchemaDoc.$defs?.[defName];
				if (resolved) {
					compare(path, resolved, zodNode);
				} else {
					diffs.push(`[${path}] Cannot resolve $ref: ${jsonNode.$ref}`);
				}
			} else {
				diffs.push(`[${path}] Unsupported $ref format: ${jsonNode.$ref}`);
			}
			return;
		}

		// Unwrap optional
		const isZodOptional = typeof zodNode.isOptional === 'function' && zodNode.isOptional();
		const actualZodNode = (isZodOptional && typeof zodNode.unwrap === 'function') 
			? zodNode.unwrap() 
			: zodNode;

		const zDefType = actualZodNode.def?.type;

		// 2. Object with explicit properties
		if (jsonNode.properties) {
			if (zDefType !== 'object' && !actualZodNode.shape) {
				diffs.push(`[${path}] JSON Schema expects object with properties, but zod is ${zDefType}`);
				return;
			}
			const zShape = actualZodNode.shape || {};
			const jProps = Object.keys(jsonNode.properties);
			const zProps = Object.keys(zShape);

			const jPropsSet = new Set(jProps);
			const zPropsSet = new Set(zProps);

			for (const p of jProps) {
				if (!zPropsSet.has(p)) {
					diffs.push(`[${path}] Property '${p}' found in JSON Schema but missing in zod`);
				}
			}
			for (const p of zProps) {
				if (!jPropsSet.has(p)) {
					diffs.push(`[${path}] Property '${p}' found in zod but missing in JSON Schema`);
				}
			}

			const jReq = new Set(jsonNode.required ?? []);
			
			for (const p of jProps) {
				if (zPropsSet.has(p)) {
					const isReqJ = jReq.has(p);
					const propZ = zShape[p];
					const isReqZ = !(typeof propZ.isOptional === 'function' && propZ.isOptional());
					if (isReqJ && !isReqZ) {
						diffs.push(`[${path}.${p}] JSON Schema requires this property, but zod makes it optional`);
					} else if (!isReqJ && isReqZ) {
						diffs.push(`[${path}.${p}] JSON Schema makes this property optional, but zod requires it`);
					}
					
					// Recurse
					const childJsonNode = jsonNode.properties[p];
					if (childJsonNode) {
						compare(`${path}.${p}`, childJsonNode, propZ);
					}
				}
			}
			return;
		}

		// 3. Free-form object (no explicit properties, additionalProperties or type: "object")
		if (jsonNode.type === 'object' || jsonNode.additionalProperties !== undefined) {
			if (zDefType !== 'object' && zDefType !== 'record') {
				diffs.push(`[${path}] JSON Schema expects free-form object, but zod is ${zDefType}`);
			}
			return;
		}

		// 4. Array
		if (jsonNode.type === 'array') {
			if (zDefType !== 'array') {
				diffs.push(`[${path}] JSON Schema expects array, but zod is ${zDefType}`);
				return;
			}
			if (jsonNode.items) {
				compare(`${path}[]`, jsonNode.items, actualZodNode.def?.element ?? actualZodNode.element);
			}
			return;
		}

		// 5. Enum
		if (jsonNode.enum) {
			if (zDefType !== 'enum') {
				diffs.push(`[${path}] JSON Schema expects enum, but zod is ${zDefType}`);
			}
			return;
		}

		// 6. Primitive types
		if (jsonNode.type) {
			const isMatch = (
				(jsonNode.type === 'string' && zDefType === 'string') ||
				(jsonNode.type === 'boolean' && zDefType === 'boolean') ||
				((jsonNode.type === 'number' || jsonNode.type === 'integer') && zDefType === 'number')
			);
			if (!isMatch) {
				diffs.push(`[${path}] Type mismatch: JSON Schema is ${jsonNode.type}, zod is ${zDefType}`);
			}
		}
	}

	compare('root', jsonSchemaDoc, zodSchema);
	return diffs;
}
