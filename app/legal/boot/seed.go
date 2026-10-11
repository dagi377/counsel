package boot

import (
	"context"
	"fmt"

	"github.com/dagi377/counsel/app/legal/comms"
	kboot "github.com/dagi377/counsel/kernel/boot"
)

// seedTemplates seeds the app's comms message templates (comms.LegalNoticeTemplates) at
// Start. TemplateStore.Seed writes every new or changed template version in one k.DB.InTx;
// unchanged text writes nothing and changed text adds version+1, never overwriting a stored
// version. A kernel built without a DB has nothing to seed into, so the step is skipped.
func seedTemplates(ctx context.Context, k *kboot.Kernel) error {
	if k.DB == nil {
		return nil
	}
	store, err := comms.NewTemplateStore(k.DB, comms.LegalNoticeTemplates())
	if err != nil {
		return fmt.Errorf("seed templates: %w", err)
	}
	if err := store.Seed(ctx); err != nil {
		return fmt.Errorf("seed templates: %w", err)
	}
	return nil
}
