// Config for the GraphQL editor extension. It reads codegen's schema and documents so the
// editor validates exactly what `pnpm codegen` compiles.
import codegen from './codegen';

const config = {
  schema: codegen.schema,
  documents: codegen.documents,
};

export default config;
