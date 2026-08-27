package vending

import (
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"math"
)

//go:embed manifest.json
var manifestJSON []byte

type Product struct {
	Key          string `json:"key"`
	Name         string `json:"name"`
	ResourceCode string `json:"resourceCode"`
	Temperature  string `json:"temperature"`
}

type Prize struct {
	Key          string `json:"key"`
	Name         string `json:"name"`
	ResourceCode string `json:"resourceCode"`
}

type Chance struct {
	Numerator   int `json:"numerator"`
	Denominator int `json:"denominator"`
}

type Machine struct {
	ID                string     `json:"id"`
	WorldID           string     `json:"worldId"`
	Model             string     `json:"model"`
	ObjectTag         string     `json:"objectTag"`
	Position          [3]float64 `json:"position"`
	RotationDegrees   [3]float64 `json:"rotationDegrees"`
	InteractionRadius float64    `json:"interactionRadius"`
	Source            string     `json:"source"`
}

type Manifest struct {
	Schema           string    `json:"schema"`
	SourceGame       string    `json:"sourceGame"`
	Currency         string    `json:"currency"`
	UnitPrice        int64     `json:"unitPrice"`
	WinningCanChance Chance    `json:"winningCanChance"`
	Products         []Product `json:"products"`
	Prize            Prize     `json:"prize"`
	Machines         []Machine `json:"machines"`
}

type Catalog struct {
	manifest   Manifest
	products   map[string]Product
	machines   map[string]Machine
	productSet []Product
}

func Load() (*Catalog, error) {
	var manifest Manifest
	if err := json.Unmarshal(manifestJSON, &manifest); err != nil {
		return nil, fmt.Errorf("decode vending manifest: %w", err)
	}
	if manifest.Schema != "new-yokosuka-vending-manifest-v1" {
		return nil, fmt.Errorf("unsupported vending schema %q", manifest.Schema)
	}
	if manifest.UnitPrice <= 0 {
		return nil, errors.New("vending price must be positive")
	}
	if manifest.WinningCanChance.Numerator <= 0 ||
		manifest.WinningCanChance.Denominator <= 0 ||
		manifest.WinningCanChance.Numerator >=
			manifest.WinningCanChance.Denominator {
		return nil, errors.New("invalid winning-can chance")
	}
	products := make(map[string]Product, len(manifest.Products))
	for _, product := range manifest.Products {
		if product.Key == "" || product.Name == "" ||
			product.ResourceCode == "" {
			return nil, errors.New("incomplete vending product")
		}
		if _, duplicate := products[product.Key]; duplicate {
			return nil, fmt.Errorf("duplicate vending product %q", product.Key)
		}
		products[product.Key] = product
	}
	machines := make(map[string]Machine, len(manifest.Machines))
	for _, machine := range manifest.Machines {
		if machine.ID == "" || machine.WorldID == "" ||
			machine.InteractionRadius <= 0 {
			return nil, errors.New("incomplete vending machine")
		}
		if _, duplicate := machines[machine.ID]; duplicate {
			return nil, fmt.Errorf("duplicate vending machine %q", machine.ID)
		}
		machines[machine.ID] = machine
	}
	if len(products) == 0 || len(machines) == 0 {
		return nil, errors.New("empty vending catalog")
	}
	return &Catalog{
		manifest:   manifest,
		products:   products,
		machines:   machines,
		productSet: append([]Product(nil), manifest.Products...),
	}, nil
}

func MustLoad() *Catalog {
	catalog, err := Load()
	if err != nil {
		panic(err)
	}
	return catalog
}

func (c *Catalog) Product(key string) (Product, bool) {
	product, ok := c.products[key]
	return product, ok
}

func (c *Catalog) Products() []Product {
	return append([]Product(nil), c.productSet...)
}

func (c *Catalog) Machine(id string) (Machine, bool) {
	machine, ok := c.machines[id]
	return machine, ok
}

func (c *Catalog) Manifest() Manifest {
	return c.manifest
}

func (c *Catalog) UnitPrice() int64 {
	return c.manifest.UnitPrice
}

func (c *Catalog) Prize() Prize {
	return c.manifest.Prize
}

func (c *Catalog) IsNear(
	machine Machine,
	worldID string,
	x,
	z float64,
) bool {
	if worldID != machine.WorldID ||
		math.IsNaN(x) || math.IsInf(x, 0) ||
		math.IsNaN(z) || math.IsInf(z, 0) {
		return false
	}
	dx := x - machine.Position[0]
	dz := z - machine.Position[2]
	return dx*dx+dz*dz <=
		machine.InteractionRadius*machine.InteractionRadius
}

func (c *Catalog) IsWinningDraw(value int) bool {
	denominator := c.manifest.WinningCanChance.Denominator
	if value < 0 || value >= denominator {
		return false
	}
	return value < c.manifest.WinningCanChance.Numerator
}
