package pbx

import "errors"

func (p *Pools) Authorize(tenant string) error {
	if tenant == "" || tenant != p.Binding.TenantID {
		return errors.New("tenant binding denied")
	}
	return nil
}
