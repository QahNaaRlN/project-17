import type { Catalog } from "../src/model.js";

export const catalog: Catalog = {
  brand: "тихая форма",
  title: "Меньше шума.",
  subtitle: "Больше дома.",
  description:
    "Тактильная керамика, мягкий свет и простые формы. Вещи, с которыми хочется жить каждый день.",
  ctaLabel: "Посмотреть коллекцию",
  collectionTitle: "Коллекция для вашего дома",
  aboutTitle: "Не торопиться. Замечать детали.",
  aboutText:
    "Выбираем вещи по тому, как они ощущаются в жизни: приятная фактура, понятная функция и форма, которая не надоедает.",
  products: [
    {
      id: "vase",
      title: "Ваза «Линия»",
      description:
        "Матовая керамическая ваза для одного цветка или небольшой композиции. Высота — 24 см. Спокойная форма и приятная на ощупь поверхность.",
      material: "Матовая поверхность",
      category: "Керамика",
      priceRub: 3200,
      tone: "sand",
    },
    {
      id: "lamp",
      title: "Лампа «Тепло»",
      description:
        "Настольная лампа с широким абажуром и мягким рассеянным светом. Для вечернего чтения и тихого уголка дома. Высота — 38 см.",
      material: "Мягкий рассеянный свет",
      category: "Свет",
      priceRub: 8900,
      tone: "sage",
    },
    {
      id: "bowl",
      title: "Чаша «Круг»",
      description:
        "Керамическая чаша тёплого оттенка. Для фруктов, небольших вещей или просто красивого акцента на столе. Диаметр — 22 см.",
      material: "Тёплый оттенок глины",
      category: "Керамика",
      priceRub: 2400,
      tone: "clay",
    },
  ],
};
const contract = {
  origin: "native",
  container: true,
  props: {
    config: {
      type: "object",
      responsive: true,
      required: true,
      fields: {
        ...Object.fromEntries(
          [
            "brand",
            "title",
            "subtitle",
            "description",
            "ctaLabel",
            "collectionTitle",
            "aboutTitle",
            "aboutText",
          ].map((name) => [name, { type: "string", required: true, maxLength: 2000 }]),
        ),
        heroAsset: { type: "asset", assetKind: "image" },
        products: {
          type: "list",
          required: true,
          max: 64,
          of: {
            type: "object",
            fields: {
              ...Object.fromEntries(
                ["id", "title", "description", "material", "category"].map((name) => [
                  name,
                  { type: "string", required: true, maxLength: 2000 },
                ]),
              ),
              tone: { type: "enum", values: ["sand", "sage", "clay"], required: true },
              priceRub: { type: "number", integer: true, min: 0, max: 1_000_000_000 },
              asset: { type: "asset", assetKind: "image" },
            },
          },
        },
      },
    },
  },
};
export const storefrontContract = {
  ...contract,
  provides: {
    store: { type: "object", required: true, fields: contract.props.config.fields },
  },
};

export const storeContracts = {
  Storefront: storefrontContract,
  ...Object.fromEntries(
    [
      ["StoreHero", ["title", "subtitle", "description", "ctaLabel"]],
      ["StoreAbout", ["title", "text"]],
    ].map(([name, fields]) => [
      name,
      {
        origin: "native",
        container: false,
        props: Object.fromEntries(
          (fields as string[]).map((field) => [
            field,
            { type: "text", content: true, required: true, maxLength: 2000 },
          ]),
        ),
      },
    ]),
  ),
  StoreCollection: {
    origin: "native",
    container: true,
    props: {
      title: { type: "text", content: true, required: true },
      products: storefrontContract.props.config.fields.products,
    },
  },
  StoreProduct: {
    origin: "native",
    container: false,
    props: {
      product: {
        ...storefrontContract.props.config.fields.products.of,
        required: true,
      },
    },
  },
  StoreEmpty: { origin: "native", container: false, props: {} },
};
