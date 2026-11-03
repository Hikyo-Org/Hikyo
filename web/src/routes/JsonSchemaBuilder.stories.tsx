import type { Meta, StoryObj } from '@storybook/react-vite';
import { useState } from 'react';
import { expect } from 'storybook/test';

import { JsonSchemaBuilder } from './JsonSchemaBuilder.tsx';

// The builder is a controlled view over schema text; this fixture owns the text.
function Example({ initial }: { initial: string }) {
  const [value, setValue] = useState(initial);
  const [error, setError] = useState<string | null>(null);
  return (
    <JsonSchemaBuilder
      value={value}
      error={error}
      onChange={(text, refusal) => {
        setValue(text);
        setError(refusal);
      }}
    />
  );
}

const nested = JSON.stringify(
  {
    type: 'object',
    properties: {
      host: { type: 'string' },
      replicas: { type: 'array', items: { type: 'object', properties: { zone: { type: 'string' } }, required: ['zone'] } },
    },
    required: ['host'],
  },
  null,
  2,
);

const ruled = JSON.stringify(
  {
    type: 'object',
    properties: { host: { type: 'string', minLength: 1, maxLength: 63, pattern: '^[a-z]' }, port: { type: 'integer' } },
    required: ['host'],
    additionalProperties: false,
  },
  null,
  2,
);

const meta = {
  title: 'Features/Values/JsonSchemaBuilder',
  id: 'routes-jsonschemabuilder',
  component: Example,
  tags: ['ai-generated'],
  args: { initial: '' },
  argTypes: { initial: { description: 'The schema text the fixture starts with; the builder edits it and reports each change as text.' } },
  parameters: {
    docs: {
      description: {
        component:
          'A no-code editor for the optional JSON Schema of a json key. It shows object fields and array items, string length and pattern rules, and closing an object to its listed fields. It writes the schema text, and never changes a schema it cannot show: those stay raw JSON under the advanced disclosure.',
      },
    },
  },
} satisfies Meta<typeof Example>;
export default meta;
type Story = StoryObj<typeof meta>;

export const Empty: Story = {
  play: async ({ canvas, userEvent }) => {
    const document = canvas.getByLabelText('Schema document');
    await expect(document).toHaveValue('');
    await userEvent.selectOptions(canvas.getByLabelText('Value is'), 'object');
    await userEvent.click(canvas.getByRole('button', { name: 'Add field' }));
    // A new field has no name yet: no error to read yet, and the text holds the last valid schema.
    await expect(canvas.queryByRole('alert')).toBeNull();
    await expect(document).toHaveValue(JSON.stringify({ type: 'object' }, null, 2));
    await userEvent.type(canvas.getByLabelText('Field name'), 'host');
    await expect(canvas.queryByRole('alert')).toBeNull();
    await expect(document).toHaveValue(
      JSON.stringify({ type: 'object', properties: { host: { type: 'string' } }, required: ['host'] }, null, 2),
    );
  },
};

export const NestedFields: Story = {
  args: { initial: nested },
  play: async ({ canvas, userEvent }) => {
    await expect(canvas.getAllByLabelText('Field name')).toHaveLength(3);
    await expect(canvas.getByLabelText('Type of replicas')).toHaveValue('array');
    await expect(canvas.getByLabelText('Required: zone')).toBeChecked();
    await expect(canvas.getByLabelText('Required: replicas')).not.toBeChecked();
    await userEvent.click(canvas.getByRole('button', { name: 'Remove field zone' }));
    await expect(canvas.getAllByLabelText('Field name')).toHaveLength(2);
    await expect(canvas.getByLabelText('Schema document')).not.toHaveValue(nested);
  },
};

export const StringRulesAndClosedObject: Story = {
  args: { initial: ruled },
  play: async ({ canvas, userEvent }) => {
    // A rule that is set opens its disclosure and shows in its label.
    await expect(canvas.getByRole('button', { name: 'Rules for host (3)' })).toHaveAttribute('aria-expanded', 'true');
    // Only string nodes have these rules.
    await expect(canvas.queryByRole('button', { name: /Rules for port/ })).toBeNull();
    await expect(canvas.getByLabelText('Min length of host')).toHaveValue('1');
    await expect(canvas.getByLabelText('Pattern of host')).toHaveValue('^[a-z]');
    await expect(canvas.getByText('RE2. Matches anywhere in the value unless you anchor it with ^ and $.')).toBeVisible();
    await expect(canvas.getByRole('checkbox', { name: 'Reject fields not listed' })).toBeChecked();

    // A length that is not a whole number is refused on its own input, and the text keeps the last valid schema.
    const document = canvas.getByLabelText('Schema document');
    const maxLength = canvas.getByLabelText('Max length of host');
    await userEvent.type(maxLength, 'x');
    await expect(canvas.getByRole('alert')).toHaveTextContent('Use a whole number of 0 or more.');
    await expect(maxLength).toBeInvalid();
    await expect(document).toHaveValue(ruled);
    await userEvent.clear(maxLength);
    await userEvent.type(maxLength, '0');
    await expect(canvas.getByRole('alert')).toHaveTextContent('Min length cannot be more than max length.');
    await userEvent.clear(maxLength);
    await userEvent.type(maxLength, '20');
    await expect(canvas.queryByRole('alert')).toBeNull();
    await expect(document).toHaveValue(ruled.replace('63', '20'));

    await userEvent.click(canvas.getByRole('checkbox', { name: 'Reject fields not listed' }));
    await expect(document).not.toHaveDisplayValue(/additionalProperties/);
  },
};

export const HandWrittenOpenObject: Story = {
  args: { initial: '{"type":"object","additionalProperties":true}' },
  play: async ({ canvas }) => {
    // Only `false` is shown: anything else is the person's own rule and stays raw, unchanged.
    await expect(canvas.getByText(/the builder cannot show/)).toBeVisible();
    await expect(canvas.queryByRole('checkbox', { name: 'Reject fields not listed' })).toBeNull();
    await expect(canvas.getByLabelText('Schema document')).toHaveValue('{"type":"object","additionalProperties":true}');
  },
};

export const NotRepresentable: Story = {
  args: { initial: '{"type":"integer","minimum":1}' },
  play: async ({ canvas }) => {
    await expect(canvas.getByText(/the builder cannot show/)).toBeVisible();
    await expect(canvas.queryByRole('button', { name: 'Add field' })).toBeNull();
    // The raw editor opens by itself and holds the schema untouched.
    await expect(canvas.getByLabelText('Schema document')).toBeVisible();
    await expect(canvas.getByLabelText('Schema document')).toHaveValue('{"type":"integer","minimum":1}');
  },
};

export const PhoneRules: Story = {
  args: { initial: ruled },
  globals: { viewport: { value: 'phone', isRotated: false } },
  play: async ({ canvasElement, canvas }) => {
    await expect(canvas.getByLabelText('Pattern of host')).toBeVisible();
    await expect(canvas.getByRole('checkbox', { name: 'Reject fields not listed' })).toBeVisible();
    await expect(canvasElement.ownerDocument.documentElement.scrollWidth).toBeLessThanOrEqual(
      canvasElement.ownerDocument.documentElement.clientWidth,
    );
  },
};

export const PhoneNested: Story = {
  args: { initial: nested },
  globals: { viewport: { value: 'phone', isRotated: false } },
  play: async ({ canvasElement, canvas }) => {
    await expect(canvas.getAllByLabelText('Field name')).toHaveLength(3);
    // Rows wrap and nested levels indent: nothing pushes the page sideways.
    await expect(canvasElement.ownerDocument.documentElement.scrollWidth).toBeLessThanOrEqual(
      canvasElement.ownerDocument.documentElement.clientWidth,
    );
  },
};
