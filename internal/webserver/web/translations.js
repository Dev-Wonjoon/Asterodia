// Language catalogs are independent so missing translations fail validation.
import ko from './langs/ko.json' with { type: 'json' };
import en from './langs/en.json' with { type: 'json' };
export const catalogs = { ko, en };
