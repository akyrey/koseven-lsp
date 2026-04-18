<?php defined('SYSPATH') or die('No direct script access.'); ?>
<h1>About</h1>
<p>Welcome, <?php echo HTML::chars($user->name); ?></p>
<?php if ($show_contact): ?>
<p>Contact: <?php echo HTML::chars($email); ?></p>
<?php endif; ?>
