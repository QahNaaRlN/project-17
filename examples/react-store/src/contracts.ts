import { t } from "@cms/component-registry";
const string = t.string().required().max(2000);
export const productField = t.object({
  id: t.string().required().max(64),
  title: t.string().required().max(200),
  description: string,
  material: string,
  category: string,
  tone: t.enum(["sand", "sage", "clay"]).required(),
  priceRub: t.number().int().min(0).max(1_000_000_000),
  asset: t.asset("image"),
});
export const productsField = t.list(productField).required().max(64);
