// short aliases over generated schema. regenerate schema with `yarn api:gen`.
import type { components, paths } from './schema.js';

export type { paths, components };
type S = components['schemas'];

export type Problem = S['Problem'];
export type FieldError = S['FieldError'];
export type Session = S['Session'];
export type User = S['User'];
export type Site = S['Site'];
export type SiteInput = S['SiteInput'];
export type SitePatch = S['SitePatch'];
export type ExtractionConfig = S['ExtractionConfig'];
export type ExtractionConfigInput = S['ExtractionConfigInput'];
export type DetailStrategy = S['DetailStrategy'];
export type Category = S['Category'];
export type CategoryInput = S['CategoryInput'];
export type CategoryPatch = S['CategoryPatch'];
export type PaginationType = S['PaginationType'];
export type LinkExtractionType = S['LinkExtractionType'];
export type Run = S['Run'];
export type RunKind = S['RunKind'];
export type RunStatus = S['RunStatus'];
export type RunList = S['RunList'];
export type SiteStats = S['SiteStats'];
export type Item = S['Item'];
export type ItemList = S['ItemList'];
